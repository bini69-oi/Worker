// Package wata это касса WATA: оплата картами и СБП по платёжной ссылке.
package wata

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bini69-oi/worker/core"
	"github.com/bini69-oi/worker/pay"
	"github.com/bini69-oi/worker/settings"
)

// DefaultBaseURL это адрес API WATA.
const DefaultBaseURL = "https://api.wata.pro/api/h2h"

var errBadSig = pay.ErrBadSignature

func init() {
	settings.AddGateway("wata", "Карты и СБП (WATA)",
		settings.Def{Key: "wata.token", Kind: settings.KindString, Secret: true, Group: "pay.wata",
			Desc: "Токен доступа WATA (Bearer) из кабинета"},
		settings.Def{Key: "wata.return_url", Kind: settings.KindString, Group: "pay.wata",
			Desc: "Куда вернуть абонента после оплаты, {payment} заменится номером платежа. Пусто: обратно в бота"},
	)
}

// Config это настройки, которые касса читает на лету.
type Config interface {
	String(key string) string
	Secret(key string) string
}

// Gateway это касса WATA.
type Gateway struct {
	cfg     Config
	baseURL string
	client  *http.Client
	now     func() time.Time
	mu      sync.Mutex
	keys    keyCache
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
	return &Gateway{cfg: cfg, baseURL: strings.TrimRight(baseURL, "/"), client: client, now: time.Now}
}

func (g *Gateway) Name() string { return "wata" }

func (g *Gateway) Configured() bool { return g.cfg.Secret("wata.token") != "" }

// link это ответ POST /links.
type link struct {
	ID  string `json:"id"`
	URL string `json:"url"`
}

// tx это транзакция WATA: и запись из API, и тело вебхука (там статус в transactionStatus).
type tx struct {
	ID            string      `json:"id"`
	TransactionID string      `json:"transactionId"`
	LinkID        string      `json:"paymentLinkId"`
	Status        string      `json:"status"`
	HookStatus    string      `json:"transactionStatus"`
	OrderID       string      `json:"orderId"`
	Amount        json.Number `json:"amount"`
	Currency      string      `json:"currency"`
}

func (t tx) status() core.PayStatus {
	if t.HookStatus != "" {
		return mapStatus(t.HookStatus)
	}
	return mapStatus(t.Status)
}

// rub отдаёт сумму оплаты в копейках. Сумма обязана быть, валюта только рубли.
func (t tx) rub() (int64, error) {
	if t.Currency != "" && !strings.EqualFold(t.Currency, "RUB") {
		return 0, fmt.Errorf("платёж WATA не в рублях: %s", t.Currency)
	}
	if t.Amount == "" {
		return 0, errors.New("в ответе WATA нет суммы")
	}
	return kopecks(string(t.Amount))
}

func (g *Gateway) Create(ctx context.Context, p core.Payment) (core.Invoice, error) {
	back := pay.ReturnURL(g.cfg.String("wata.return_url"), g.Return, p.ID)
	in := map[string]any{
		"type":        "OneTime",
		"amount":      json.Number(rubles(p.Amount)),
		"currency":    "RUB",
		"description": fmt.Sprintf("Пополнение %d", p.ID),
		"orderId":     strconv.FormatInt(p.ID, 10),
	}
	if back != "" {
		in["successRedirectUrl"], in["failRedirectUrl"] = back, back
	}
	var l link
	if err := g.do(ctx, http.MethodPost, "/links", in, &l); err != nil {
		return core.Invoice{}, err
	}
	if l.ID == "" || l.URL == "" {
		return core.Invoice{}, errors.New("касса WATA не вернула номер ссылки или адрес")
	}
	return core.Invoice{ExternalID: l.ID, URL: l.URL, ExtAmount: p.Amount, ExtCurrency: "RUB"}, nil
}

// Status ищет у WATA транзакции по нашему номеру платежа. Оплаченная есть: платёж оплачен.
// Неудачная попытка ссылку не закрывает, абонент может повторить, поэтому без оплаты это ожидание.
func (g *Gateway) Status(ctx context.Context, p core.Payment) (core.GatewayEvent, error) {
	order := strconv.FormatInt(p.ID, 10)
	var res struct {
		Items []tx `json:"items"`
	}
	q := url.Values{"orderId": {order}, "statuses": {"Paid"}, "maxResultCount": {"5"}}
	if err := g.do(ctx, http.MethodGet, "/transactions?"+q.Encode(), nil, &res); err != nil {
		return core.GatewayEvent{}, err
	}
	ev := core.GatewayEvent{ExternalID: p.ExternalID, PaymentID: p.ID, Status: core.PayPending}
	for _, t := range res.Items {
		if t.OrderID != order || t.status() != core.PayPaid {
			continue
		}
		var err error
		ev.Status = core.PayPaid
		ev.ExtAmount, err = t.rub()
		return ev, err
	}
	return ev, nil
}

// Verify проверяет RSA-подпись в заголовке X-Signature по сырому телу и разбирает вебхук.
// Деньги берутся из подписанного тела: подделать его без закрытого ключа WATA нельзя.
func (g *Gateway) Verify(r *http.Request) (core.GatewayEvent, error) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		return core.GatewayEvent{}, errors.New("тело вебхука WATA не прочитано")
	}
	if err := g.checkSignature(r.Context(), body, r.Header.Get("X-Signature")); err != nil {
		return core.GatewayEvent{}, err
	}
	var hook tx
	if err := json.Unmarshal(body, &hook); err != nil {
		return core.GatewayEvent{}, errors.New("вебхук кассы WATA не разобран")
	}
	id, err := strconv.ParseInt(hook.OrderID, 10, 64)
	if err != nil || id <= 0 {
		return core.GatewayEvent{}, errors.New("в вебхуке WATA нет нашего номера платежа")
	}
	// Номер ссылки кладём как внешний, только если WATA его прислала: у транзакции он другой.
	ev := core.GatewayEvent{PaymentID: id, ExternalID: hook.LinkID, Status: hook.status()}
	if ev.Status == core.PayPaid {
		ev.ExtAmount, err = hook.rub()
	}
	return ev, err
}

// mapStatus переводит статус транзакции WATA: Paid оплачена, Declined отклонена, остальное ждёт.
func mapStatus(s string) core.PayStatus {
	switch strings.ToLower(s) {
	case "paid":
		return core.PayPaid
	case "declined":
		return core.PayFailed
	}
	return core.PayPending
}

func (g *Gateway) newRequest(ctx context.Context, method, path string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, g.baseURL+path, body)
	if err != nil {
		return nil, errors.New("не собран запрос к WATA")
	}
	req.Header.Set("Authorization", "Bearer "+g.cfg.Secret("wata.token"))
	req.Header.Set("Content-Type", "application/json")
	return req, nil
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
	req, err := g.newRequest(ctx, method, path, body)
	if err != nil {
		return err
	}
	resp, err := g.client.Do(req)
	if err != nil {
		return errors.New("касса WATA недоступна")
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("касса WATA ответила кодом %d", resp.StatusCode)
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(out); err != nil {
		return errors.New("ответ кассы WATA не разобран")
	}
	return nil
}
