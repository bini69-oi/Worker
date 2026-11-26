// Package paypear это касса PayPear (paypear.ru): оплата по ссылке, СБП или карта.
//
// Запросы идут с Basic-авторизацией (ID магазина и секретный ключ). Способ подписи вебхуков в документации
// не описан, поэтому вебхуку не верим вовсе: он только подсказывает номер платежа, а статус и сумму
// касса переспрашивает у API и верит только ему.
package paypear

import (
	"bytes"
	"context"
	"crypto/rand"
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

// DefaultBaseURL это адрес API PayPear.
const DefaultBaseURL = "https://api.paypear.ru/v1"

const (
	keyShop   = "paypear.shop_id"
	keySecret = "paypear.secret_key"
	keyMethod = "paypear.method"
	keyReturn = "paypear.return_url"
	keyHook   = "paypear.hook_url"

	// orderPrefix метит наш номер платежа в order_id, чтобы узнать его в вебхуке.
	orderPrefix = "worker_"
	maxBody     = 1 << 20
)

func init() {
	settings.AddGateway("paypear", "СБП и карты (PayPear)",
		settings.Def{Key: keyShop, Kind: settings.KindString, Secret: true, Group: "pay.paypear",
			Desc: "ID магазина из кабинета PayPear"},
		settings.Def{Key: keySecret, Kind: settings.KindString, Secret: true, Group: "pay.paypear",
			Desc: "Секретный ключ магазина из кабинета PayPear"},
		settings.Def{Key: keyMethod, Kind: settings.KindString, Group: "pay.paypear", Default: "sbp",
			Desc: "Способ оплаты: sbp, bank_card, sberpay или tpay"},
		settings.Def{Key: keyHook, Kind: settings.KindString, Group: "pay.paypear",
			Desc: "Адрес вебхука для счетов: https://ваш-сайт/pay/paypear. Пусто: подставит сборка"},
		settings.Def{Key: keyReturn, Kind: settings.KindString, Group: "pay.paypear",
			Desc: "Куда вернуть абонента после оплаты, {payment} заменится номером платежа. Пусто: обратно в бота"},
	)
}

// Config это настройки, которые касса читает на лету.
type Config interface {
	String(key string) string
	Secret(key string) string
}

// Gateway это касса PayPear.
type Gateway struct {
	cfg     Config
	baseURL string
	client  *http.Client
	// Return это адрес возврата по умолчанию, если настройка пустая. Ставит сборка: диплинк в бота.
	Return func(paymentID int64) string
	// Hook это адрес вебхука по умолчанию, если настройка пустая. Ставит сборка: PUBLIC_URL/pay/paypear.
	Hook string
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

func (g *Gateway) Name() string { return "paypear" }

func (g *Gateway) Configured() bool {
	return g.cfg.Secret(keyShop) != "" && g.cfg.Secret(keySecret) != ""
}

// money это сумма: объект {"value":"300.00","currency":"RUB"} или просто число.
type money struct {
	Value    json.Number
	Currency string
}

func (m *money) UnmarshalJSON(b []byte) error {
	var obj struct {
		Value    json.Number `json:"value"`
		Currency string      `json:"currency"`
	}
	if err := json.Unmarshal(b, &obj); err == nil {
		m.Value, m.Currency = obj.Value, obj.Currency
		return nil
	}
	return json.Unmarshal(b, &m.Value)
}

// kopecks отдаёт сумму в копейках: сумма обязана быть, валюта, если указана, только рубли.
func (m money) kopecks() (int64, error) {
	if m.Currency != "" && !strings.EqualFold(m.Currency, "RUB") {
		return 0, fmt.Errorf("платёж PayPear не в рублях: %s", m.Currency)
	}
	if m.Value == "" {
		return 0, errors.New("в ответе PayPear нет суммы")
	}
	return pay.Kopecks(string(m.Value))
}

// object это платёж в ответе API и в вебхуке.
type object struct {
	ID           string `json:"id"`
	OrderID      string `json:"order_id"`
	Status       string `json:"status"`
	Amount       money  `json:"amount"`
	Confirmation struct {
		URL  string `json:"confirmation_url"`
		Alt  string `json:"url"`
		Type string `json:"type"`
	} `json:"confirmation"`
}

type envelope struct {
	Success bool   `json:"success"`
	Result  object `json:"result"`
}

func (g *Gateway) Create(ctx context.Context, p core.Payment) (core.Invoice, error) {
	method := strings.TrimSpace(g.cfg.String(keyMethod))
	if method == "" {
		method = "sbp"
	}
	req := map[string]any{
		"amount":              map[string]string{"value": pay.Rubles(p.Amount), "currency": "RUB"},
		"order_id":            orderPrefix + strconv.FormatInt(p.ID, 10),
		"description":         fmt.Sprintf("Пополнение %d", p.ID),
		"payment_method_data": map[string]string{"type": method},
	}
	if back := pay.ReturnURL(g.cfg.String(keyReturn), g.Return, p.ID); back != "" {
		req["confirmation"] = map[string]string{"type": "redirect", "return_url": back}
	}
	hook := strings.TrimSpace(g.cfg.String(keyHook))
	if hook == "" {
		hook = g.Hook
	}
	if hook != "" {
		req["webhook_url"] = hook
	}
	res, err := g.do(ctx, http.MethodPost, "/payment/", req)
	if err != nil {
		return core.Invoice{}, err
	}
	link := res.Confirmation.URL
	if link == "" {
		link = res.Confirmation.Alt
	}
	if res.ID == "" || link == "" {
		return core.Invoice{}, errors.New("касса PayPear не вернула номер платежа или ссылку")
	}
	// PayPear может добавить комиссию к сумме: ждём в вебхуке именно списанную сумму.
	charged, err := res.Amount.kopecks()
	if err != nil {
		return core.Invoice{}, err
	}
	return core.Invoice{ExternalID: res.ID, URL: link, ExtAmount: charged, ExtCurrency: "RUB"}, nil
}

func (g *Gateway) Status(ctx context.Context, p core.Payment) (core.GatewayEvent, error) {
	return g.confirm(ctx, p.ExternalID)
}

// confirm спрашивает у API статус платежа и сумму, если он оплачен.
func (g *Gateway) confirm(ctx context.Context, id string) (core.GatewayEvent, error) {
	res, err := g.do(ctx, http.MethodGet, "/payment/"+url.PathEscape(id)+"/", nil)
	if err != nil {
		return core.GatewayEvent{}, err
	}
	// Платёж ядро находит по номеру кассы, который сохранён при создании счёта.
	ev := core.GatewayEvent{ExternalID: id, Status: mapStatus(res.Status)}
	if ev.Status == core.PayPaid {
		ev.ExtAmount, err = res.Amount.kopecks()
	}
	return ev, err
}

// Verify разбирает вебхук {"event":..., "object":{...}} и сверяет его с API. Подписи нет: верим только API.
// Платёж, которого касса не знает, или не оплаченный по её ответу, возвращает ошибку.
func (g *Gateway) Verify(r *http.Request) (core.GatewayEvent, error) {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxBody))
	if err != nil {
		return core.GatewayEvent{}, errors.New("вебхук кассы PayPear не прочитан")
	}
	var hook struct {
		Object object `json:"object"`
	}
	if err := json.Unmarshal(body, &hook); err != nil || hook.Object.ID == "" || !g.Configured() {
		return core.GatewayEvent{}, pay.ErrBadSignature
	}
	ev, err := g.confirm(r.Context(), hook.Object.ID)
	if err != nil {
		return core.GatewayEvent{}, pay.ErrBadSignature
	}
	if ev.Status != core.PayPaid && ev.Status != core.PayRefunded {
		return ev, errors.New("касса PayPear не подтвердила оплату из вебхука")
	}
	if ev.Status == core.PayPaid && hook.Object.Amount.Value != "" {
		if got, err := hook.Object.Amount.kopecks(); err != nil || got != ev.ExtAmount {
			ev.ExtAmount = -1 // сумма вебхука не сошлась с API: ядро отклонит зачисление
		}
	}
	return ev, nil
}

func mapStatus(s string) core.PayStatus {
	switch strings.ToUpper(s) {
	case "CONFIRMED":
		return core.PayPaid
	case "CANCELED", "EXPIRED":
		return core.PayFailed
	case "REFUNDED":
		return core.PayRefunded
	}
	return core.PayPending // NEW, PROCESS
}

// do шлёт запрос в API. Ключи идут только в заголовок и в ошибки не попадают.
func (g *Gateway) do(ctx context.Context, method, path string, in any) (object, error) {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return object{}, err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, g.baseURL+path, body)
	if err != nil {
		return object{}, errors.New("не собран запрос к PayPear")
	}
	req.SetBasicAuth(g.cfg.Secret(keyShop), g.cfg.Secret(keySecret))
	req.Header.Set("Content-Type", "application/json")
	if method == http.MethodPost {
		key := make([]byte, 16)
		_, _ = rand.Read(key)
		req.Header.Set("Idempotence-Key", hex.EncodeToString(key))
	}
	resp, err := g.client.Do(req)
	if err != nil {
		return object{}, errors.New("касса PayPear недоступна")
	}
	defer resp.Body.Close()
	var out envelope
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxBody)).Decode(&out); err != nil && resp.StatusCode < 300 {
		return object{}, errors.New("ответ кассы PayPear не разобран")
	}
	if resp.StatusCode >= 300 || !out.Success {
		return object{}, fmt.Errorf("касса PayPear ответила кодом %d", resp.StatusCode)
	}
	return out.Result, nil
}
