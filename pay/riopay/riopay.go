// Package riopay это касса RioPay (api.riopay.online): оплата по ссылке, вебхук с подписью HMAC-SHA512 по телу.
package riopay

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/bini69-oi/worker/core"
	"github.com/bini69-oi/worker/pay"
	"github.com/bini69-oi/worker/settings"
)

// DefaultBaseURL это адрес API RioPay.
const DefaultBaseURL = "https://api.riopay.online/v1"

const (
	keyToken  = "riopay.api_token"
	keySecret = "riopay.webhook_secret"
	keyReturn = "riopay.return_url"
)

func init() {
	settings.AddGateway("riopay", "RioPay",
		settings.Def{Key: keyToken, Kind: settings.KindString, Secret: true, Group: "pay.riopay",
			Desc: "API-токен из кабинета RioPay (заголовок X-Api-Token)"},
		settings.Def{Key: keySecret, Kind: settings.KindString, Secret: true, Group: "pay.riopay",
			Desc: "Ключ подписи вебхуков. Пусто: подписывает API-токен"},
		settings.Def{Key: keyReturn, Kind: settings.KindString, Group: "pay.riopay",
			Desc: "Куда вернуть абонента после оплаты, {payment} заменится номером платежа. Пусто: обратно в бота"},
	)
}

// Config это настройки, которые касса читает на лету.
type Config interface {
	String(key string) string
	Secret(key string) string
}

// Gateway это касса RioPay.
type Gateway struct {
	cfg     Config
	baseURL string
	client  *http.Client
	// Return это адрес возврата по умолчанию, если настройка пустая. Ставит сборка: диплинк в бота.
	Return func(paymentID int64) string
}

var _ pay.Gateway = (*Gateway)(nil)

// New собирает кассу. Пустой baseURL означает боевой адрес, пустой client означает клиент с таймаутом.
func New(cfg Config, baseURL string, client *http.Client) *Gateway {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	return &Gateway{cfg: cfg, baseURL: strings.TrimRight(baseURL, "/"), client: client}
}

func (g *Gateway) Name() string { return "riopay" }

func (g *Gateway) Configured() bool { return g.cfg.Secret(keyToken) != "" }

type createReq struct {
	Amount     string `json:"amount"`
	ExternalID string `json:"externalId"`
	Purpose    string `json:"purpose"`
	SuccessURL string `json:"successUrl,omitempty"`
}

// order это общие поля ответа API и вебхука.
type order struct {
	ID          string      `json:"id"`
	ExternalID  string      `json:"externalId"`
	Status      string      `json:"status"`
	Amount      json.Number `json:"amount"`
	Currency    string      `json:"currency"`
	PaymentLink string      `json:"paymentLink"`
}

// rubAmount отдаёт сумму заказа: она обязана быть, валюта, если указана, только рубли.
func (o order) rubAmount() (int64, error) {
	if o.Currency != "" && !strings.EqualFold(o.Currency, "RUB") {
		return 0, fmt.Errorf("заказ RioPay не в рублях: %s", o.Currency)
	}
	if o.Amount == "" {
		return 0, errors.New("в ответе RioPay нет суммы")
	}
	return pay.Kopecks(string(o.Amount))
}

func (g *Gateway) Create(ctx context.Context, p core.Payment) (core.Invoice, error) {
	req := createReq{
		Amount:     pay.Rubles(p.Amount),
		ExternalID: strconv.FormatInt(p.ID, 10),
		Purpose:    fmt.Sprintf("Пополнение %d", p.ID),
		SuccessURL: pay.ReturnURL(g.cfg.String(keyReturn), g.Return, p.ID),
	}
	var out order
	if err := g.do(ctx, http.MethodPost, "/orders", req, &out); err != nil {
		return core.Invoice{}, err
	}
	if out.ID == "" || out.PaymentLink == "" {
		return core.Invoice{}, errors.New("касса RioPay не вернула номер заказа или ссылку")
	}
	return core.Invoice{ExternalID: out.ID, URL: out.PaymentLink, ExtAmount: p.Amount, ExtCurrency: "RUB"}, nil
}

func (g *Gateway) Status(ctx context.Context, p core.Payment) (core.GatewayEvent, error) {
	remote, err := g.fetch(ctx, p.ExternalID)
	if err != nil {
		return core.GatewayEvent{}, err
	}
	ev := core.GatewayEvent{ExternalID: p.ExternalID, Status: mapStatus(remote.Status)}
	if ev.Status == core.PayPaid {
		ev.ExtAmount, err = remote.rubAmount()
	}
	return ev, err
}

func (g *Gateway) fetch(ctx context.Context, id string) (order, error) {
	var out order
	err := g.do(ctx, http.MethodGet, "/orders/"+url.PathEscape(id), nil, &out)
	return out, err
}

// Verify проверяет подпись вебхука: X-Signature это hex HMAC-SHA512 от тела. Оплату затем подтверждает API:
// статус и сумма берутся оттуда, вебхуку верим только в номере заказа.
func (g *Gateway) Verify(r *http.Request) (core.GatewayEvent, error) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		return core.GatewayEvent{}, errors.New("вебхук кассы RioPay не прочитан")
	}
	secret := g.cfg.Secret(keySecret)
	if secret == "" {
		secret = g.cfg.Secret(keyToken)
	}
	got, err := hex.DecodeString(strings.TrimSpace(r.Header.Get("X-Signature")))
	if secret == "" || err != nil || !hmac.Equal(got, sign(body, secret)) {
		return core.GatewayEvent{}, pay.ErrBadSignature
	}
	var hook order
	if err := json.Unmarshal(body, &hook); err != nil {
		return core.GatewayEvent{}, errors.New("вебхук кассы RioPay не разобран")
	}
	ev := core.GatewayEvent{ExternalID: hook.ID, Status: mapStatus(hook.Status)}
	ev.PaymentID, _ = strconv.ParseInt(hook.ExternalID, 10, 64)
	if ev.ExternalID == "" {
		return ev, errors.New("в вебхуке кассы RioPay нет номера заказа")
	}
	if ev.Status != core.PayPaid {
		return ev, nil
	}
	remote, err := g.fetch(r.Context(), ev.ExternalID)
	if err != nil {
		return ev, err
	}
	if mapStatus(remote.Status) != core.PayPaid {
		return ev, errors.New("касса RioPay не подтвердила оплату из вебхука")
	}
	if ev.ExtAmount, err = remote.rubAmount(); err != nil {
		return ev, err
	}
	if hookAmount, err := pay.Kopecks(string(hook.Amount)); err != nil || hookAmount != ev.ExtAmount {
		ev.ExtAmount = hookAmount // сумма вебхука не сошлась с API: ядро отклонит зачисление
	}
	return ev, nil
}

// sign считает подпись вебхука.
func sign(body []byte, secret string) []byte {
	m := hmac.New(sha512.New, []byte(secret))
	m.Write(body)
	return m.Sum(nil)
}

func mapStatus(s string) core.PayStatus {
	switch strings.ToUpper(s) {
	case "COMPLETED":
		return core.PayPaid
	case "CANCELED", "CANCELLED", "FAILED", "EXPIRED":
		return core.PayFailed
	}
	return core.PayPending
}

// do шлёт запрос в API. Токен идёт только в заголовок и в ошибки не попадает.
func (g *Gateway) do(ctx context.Context, method, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, g.baseURL+path, body)
	if err != nil {
		return errors.New("не собран запрос к RioPay")
	}
	req.Header.Set("X-Api-Token", g.cfg.Secret(keyToken))
	req.Header.Set("Content-Type", "application/json")
	resp, err := g.client.Do(req)
	if err != nil {
		return errors.New("касса RioPay недоступна")
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("касса RioPay ответила кодом %d", resp.StatusCode)
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(out); err != nil {
		return errors.New("ответ кассы RioPay не разобран")
	}
	return nil
}
