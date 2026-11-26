// Package cispay это касса CisPay: карта и СБП по ссылке, суммы в копейках, вебхук с подписью HMAC-SHA256.
package cispay

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
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

// DefaultBaseURL это адрес API CisPay.
const DefaultBaseURL = "https://api.cispay.app"

// orderPrefix метит наш номер платежа в order_id, чтобы узнать его в вебхуке.
const orderPrefix = "worker:"

const (
	keyShop   = "cispay.shop_id"
	keyAPI    = "cispay.api_key"
	keyMethod = "cispay.method"
	keyReturn = "cispay.return_url"
)

func init() {
	settings.AddGateway("cispay", "CisPay",
		settings.Def{Key: keyShop, Kind: settings.KindString, Secret: true, Group: "pay.cispay",
			Desc: "UUID магазина из кабинета CisPay (заголовок X-Shop-ID)"},
		settings.Def{Key: keyAPI, Kind: settings.KindString, Secret: true, Group: "pay.cispay",
			Desc: "Секретный ключ из кабинета CisPay (cis_sec_..., он же ключ подписи вебхуков)"},
		settings.Def{Key: keyMethod, Kind: settings.KindString, Group: "pay.cispay",
			Desc: "Способ оплаты: CARD или SBP. Пусто: CARD"},
		settings.Def{Key: keyReturn, Kind: settings.KindString, Group: "pay.cispay",
			Desc: "Куда вернуть абонента после оплаты, {payment} заменится номером платежа. Пусто: обратно в бота"},
	)
}

// Config это настройки, которые касса читает на лету.
type Config interface {
	String(key string) string
	Secret(key string) string
}

// Gateway это касса CisPay.
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

func (g *Gateway) Name() string { return "cispay" }

func (g *Gateway) Configured() bool {
	return g.cfg.Secret(keyShop) != "" && g.cfg.Secret(keyAPI) != ""
}

type createReq struct {
	Amount     int64  `json:"amount"`
	Currency   string `json:"currency"`
	OrderID    string `json:"order_id"`
	Method     string `json:"payment_method"`
	CustomerID string `json:"customer_id"`
	Desc       string `json:"description"`
	Success    string `json:"redirect_success_url,omitempty"`
	Fail       string `json:"redirect_fail_url,omitempty"`
}

// apiPay это общие поля ответа API и вебхука. Сумма в копейках целым числом.
type apiPay struct {
	ID         string      `json:"id"`
	OrderID    string      `json:"order_id"`
	Status     string      `json:"status"`
	Amount     json.Number `json:"amount"`
	PaymentURL string      `json:"payment_url"`
}

// kopecks отдаёт сумму: целое число копеек, дробь и пустое значение это ошибка.
func (a apiPay) kopecks() (int64, error) {
	n, err := strconv.ParseInt(string(a.Amount), 10, 64)
	if err != nil || n <= 0 {
		return 0, errors.New("в ответе CisPay нет суммы в копейках")
	}
	return n, nil
}

func (g *Gateway) Create(ctx context.Context, p core.Payment) (core.Invoice, error) {
	method := strings.ToUpper(strings.TrimSpace(g.cfg.String(keyMethod)))
	if method != "SBP" {
		method = "CARD"
	}
	back := pay.ReturnURL(g.cfg.String(keyReturn), g.Return, p.ID)
	req := createReq{
		Amount:     p.Amount,
		Currency:   "RUB",
		OrderID:    orderPrefix + strconv.FormatInt(p.ID, 10),
		Method:     method,
		CustomerID: strconv.FormatInt(p.UserID, 10), // СБП требует устойчивый номер плательщика
		Desc:       fmt.Sprintf("Пополнение %d", p.ID),
		Success:    back,
		Fail:       back,
	}
	var out apiPay
	if err := g.do(ctx, http.MethodPost, "/payments", req, &out); err != nil {
		return core.Invoice{}, err
	}
	if out.ID == "" || out.PaymentURL == "" {
		return core.Invoice{}, errors.New("касса CisPay не вернула номер платежа или ссылку")
	}
	return core.Invoice{ExternalID: out.ID, URL: out.PaymentURL, ExtAmount: p.Amount, ExtCurrency: "RUB"}, nil
}

func (g *Gateway) Status(ctx context.Context, p core.Payment) (core.GatewayEvent, error) {
	remote, err := g.fetch(ctx, p.ExternalID)
	if err != nil {
		return core.GatewayEvent{}, err
	}
	ev := core.GatewayEvent{ExternalID: p.ExternalID, Status: mapStatus(remote.Status)}
	if ev.Status == core.PayPaid {
		ev.ExtAmount, err = remote.kopecks()
	}
	return ev, err
}

func (g *Gateway) fetch(ctx context.Context, id string) (apiPay, error) {
	var out apiPay
	err := g.do(ctx, http.MethodGet, "/payments/status?id="+url.QueryEscape(id), nil, &out)
	return out, err
}

// Verify проверяет подпись вебхука: X-Signature это hex HMAC-SHA256 от сырого тела с секретным ключом магазина.
// Оплату и возврат затем подтверждает API: статус и сумма берутся оттуда.
func (g *Gateway) Verify(r *http.Request) (core.GatewayEvent, error) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		return core.GatewayEvent{}, errors.New("вебхук кассы CisPay не прочитан")
	}
	key := g.cfg.Secret(keyAPI)
	got, err := hex.DecodeString(strings.TrimSpace(r.Header.Get("X-Signature")))
	if key == "" || err != nil || !hmac.Equal(got, sign(body, key)) {
		return core.GatewayEvent{}, pay.ErrBadSignature
	}
	var hook apiPay
	if err := json.Unmarshal(body, &hook); err != nil {
		return core.GatewayEvent{}, errors.New("вебхук кассы CisPay не разобран")
	}
	ev := core.GatewayEvent{ExternalID: hook.ID, Status: mapStatus(hook.Status)}
	if n, ok := strings.CutPrefix(hook.OrderID, orderPrefix); ok {
		ev.PaymentID, _ = strconv.ParseInt(n, 10, 64)
	}
	if ev.ExternalID == "" {
		return ev, errors.New("в вебхуке кассы CisPay нет номера платежа")
	}
	if ev.Status != core.PayPaid && ev.Status != core.PayRefunded {
		return ev, nil
	}
	remote, err := g.fetch(r.Context(), ev.ExternalID)
	if err != nil {
		return ev, err
	}
	if mapStatus(remote.Status) != ev.Status {
		return ev, errors.New("касса CisPay не подтвердила статус из вебхука")
	}
	if ev.Status == core.PayRefunded {
		return ev, nil
	}
	if ev.ExtAmount, err = remote.kopecks(); err != nil {
		return ev, err
	}
	if hookAmount, err := hook.kopecks(); err == nil && hookAmount != ev.ExtAmount {
		ev.ExtAmount = hookAmount // сумма вебхука не сошлась с API: ядро отклонит зачисление
	}
	return ev, nil
}

// sign считает подпись вебхука.
func sign(body []byte, key string) []byte {
	m := hmac.New(sha256.New, []byte(key))
	m.Write(body)
	return m.Sum(nil)
}

func mapStatus(s string) core.PayStatus {
	switch strings.ToUpper(s) {
	case "PAID":
		return core.PayPaid
	case "FAILED", "EXPIRED":
		return core.PayFailed
	case "REFUNDED":
		return core.PayRefunded // возврат после оплаты: ядро отменит зачисление
	}
	return core.PayPending
}

// do шлёт запрос в API. Ключи идут только в заголовки и в ошибки не попадают.
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
		return errors.New("не собран запрос к CisPay")
	}
	req.Header.Set("X-Shop-ID", g.cfg.Secret(keyShop))
	req.Header.Set("X-Api-Key", g.cfg.Secret(keyAPI))
	req.Header.Set("Content-Type", "application/json")
	resp, err := g.client.Do(req)
	if err != nil {
		return errors.New("касса CisPay недоступна")
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("касса CisPay ответила кодом %d", resp.StatusCode)
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(out); err != nil {
		return errors.New("ответ кассы CisPay не разобран")
	}
	return nil
}
