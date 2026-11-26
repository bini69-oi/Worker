// Package severpay это касса SeverPay (severpay.io): оплата картой и СБП по ссылке.
//
// Запросы и вебхуки подписаны одинаково: HMAC-SHA256 ключом кассы от JSON с отсортированными ключами
// верхнего уровня без пробелов, подпись лежит в поле sign тела. В запрос добавляются mid и случайный salt.
package severpay

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/bini69-oi/worker/core"
	"github.com/bini69-oi/worker/pay"
	"github.com/bini69-oi/worker/settings"
)

// DefaultBaseURL это адрес API SeverPay.
const DefaultBaseURL = "https://severpay.io/api/merchant"

const (
	keyMID    = "severpay.mid"
	keyToken  = "severpay.token"
	keyReturn = "severpay.return_url"

	// orderPrefix метит наш номер платежа в order_id, чтобы узнать его в вебхуке.
	orderPrefix = "worker_"
	maxBody     = 1 << 20
	// lifetime это срок жизни ссылки на оплату в минутах.
	lifetime = 1440
)

func init() {
	settings.AddGateway("severpay", "СБП и карты (SeverPay)",
		settings.Def{Key: keyMID, Kind: settings.KindString, Secret: true, Group: "pay.severpay",
			Desc: "ID мерчанта (mid) из кабинета SeverPay, число"},
		settings.Def{Key: keyToken, Kind: settings.KindString, Secret: true, Group: "pay.severpay",
			Desc: "Токен мерчанта: им подписываются запросы и вебхуки"},
		settings.Def{Key: keyReturn, Kind: settings.KindString, Group: "pay.severpay",
			Desc: "Куда вернуть абонента после оплаты, {payment} заменится номером платежа. Пусто: обратно в бота"},
	)
}

// Config это настройки, которые касса читает на лету.
type Config interface {
	String(key string) string
	Secret(key string) string
}

// Gateway это касса SeverPay.
type Gateway struct {
	cfg     Config
	baseURL string
	client  *http.Client
	// Return это адрес возврата по умолчанию, если настройка пустая. Ставит сборка: диплинк в бота.
	Return func(paymentID int64) string
}

var (
	_ pay.Gateway = (*Gateway)(nil)
	_ pay.Acker   = (*Gateway)(nil)
)

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

func (g *Gateway) Name() string { return "severpay" }

func (g *Gateway) Configured() bool {
	_, ok := g.mid()
	return ok && g.cfg.Secret(keyToken) != ""
}

// Ack это слово в ответе на вебхук: без {"status":true} SeverPay повторяет уведомление.
func (g *Gateway) Ack(core.GatewayEvent) string { return `{"status":true}` }

// mid это номер мерчанта числом: в тело он уходит числом.
func (g *Gateway) mid() (int64, bool) {
	n, err := strconv.ParseInt(strings.TrimSpace(g.cfg.Secret(keyMID)), 10, 64)
	return n, err == nil && n > 0
}

// flex это поле, которое касса шлёт то строкой, то числом (id платежа).
type flex string

func (f *flex) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	if s == "null" {
		s = ""
	}
	*f = flex(s)
	return nil
}

// payin это платёж в ответах API и в вебхуке.
type payin struct {
	ID       flex        `json:"id"`
	OrderID  string      `json:"order_id"`
	Status   string      `json:"status"`
	Amount   json.Number `json:"amount"`
	Currency string      `json:"currency"`
	URL      string      `json:"url"`
}

// reply это ответ API: полезные данные лежат в data.
type reply struct {
	Status bool  `json:"status"`
	Data   payin `json:"data"`
}

func (g *Gateway) Create(ctx context.Context, p core.Payment) (core.Invoice, error) {
	body := map[string]json.RawMessage{
		"order_id":     quote(orderPrefix + strconv.FormatInt(p.ID, 10)),
		"amount":       json.RawMessage(rubles(p.Amount)),
		"currency":     quote("RUB"),
		"client_email": quote(fmt.Sprintf("user%d@telegram.org", p.UserID)),
		"client_id":    quote(strconv.FormatInt(p.UserID, 10)),
		"lifetime":     json.RawMessage(strconv.Itoa(lifetime)),
	}
	if back := pay.ReturnURL(g.cfg.String(keyReturn), g.Return, p.ID); back != "" {
		body["url_return"] = quote(back)
	}
	res, err := g.call(ctx, "/payin/create", body)
	if err != nil {
		return core.Invoice{}, err
	}
	if res.Data.ID == "" || res.Data.URL == "" {
		return core.Invoice{}, errors.New("касса SeverPay не вернула номер платежа или ссылку")
	}
	return core.Invoice{ExternalID: string(res.Data.ID), URL: res.Data.URL, ExtAmount: p.Amount, ExtCurrency: "RUB"}, nil
}

func (g *Gateway) Status(ctx context.Context, p core.Payment) (core.GatewayEvent, error) {
	return g.fetch(ctx, p.ExternalID, p.ID)
}

// fetch спрашивает у API платёж по номеру кассы. Статус и сумма берутся только оттуда.
func (g *Gateway) fetch(ctx context.Context, id string, paymentID int64) (core.GatewayEvent, error) {
	if id == "" {
		return core.GatewayEvent{}, errors.New("у платежа нет номера в кассе SeverPay")
	}
	res, err := g.call(ctx, "/payin/get", map[string]json.RawMessage{"id": quote(id)})
	if err != nil {
		return core.GatewayEvent{}, err
	}
	return event(res.Data, id, paymentID)
}

// event превращает платёж кассы в событие ядра. Сумма нужна только у оплаченного.
func event(d payin, id string, paymentID int64) (core.GatewayEvent, error) {
	ev := core.GatewayEvent{PaymentID: paymentID, ExternalID: id, Status: mapStatus(d.Status)}
	if ev.Status != core.PayPaid {
		return ev, nil
	}
	if d.Currency != "" && !strings.EqualFold(d.Currency, "RUB") {
		return ev, fmt.Errorf("платёж SeverPay не в рублях: %s", d.Currency)
	}
	if d.Amount == "" {
		return ev, errors.New("в данных SeverPay нет суммы")
	}
	var err error
	ev.ExtAmount, err = kopecks(string(d.Amount))
	return ev, err
}

// Verify проверяет sign вебхука: пробует канон с исходным порядком вложенных полей и с отсортированным.
// Вебхук приходит как {"type":"payin","data":{...},"sign":"..."}, плоское тело тоже понимается.
func (g *Gateway) Verify(r *http.Request) (core.GatewayEvent, error) {
	token := g.cfg.Secret(keyToken)
	raw, err := io.ReadAll(io.LimitReader(r.Body, maxBody))
	if err != nil {
		return core.GatewayEvent{}, errors.New("вебхук кассы SeverPay не прочитан")
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		return core.GatewayEvent{}, errors.New("вебхук кассы SeverPay не разобран")
	}
	var got string
	_ = json.Unmarshal(top["sign"], &got)
	if token == "" || got == "" {
		return core.GatewayEvent{}, pay.ErrBadSignature
	}
	flat, deep := equalSign(got, mac(canon(top, false), token)), equalSign(got, mac(canon(top, true), token))
	if !flat && !deep {
		return core.GatewayEvent{}, pay.ErrBadSignature
	}
	var typ string
	_ = json.Unmarshal(top["type"], &typ)
	if typ != "" && typ != "payin" {
		return core.GatewayEvent{}, errors.New("вебхук SeverPay не про приём платежа")
	}
	data := top["data"]
	if len(data) == 0 {
		data = raw
	}
	var d payin
	if err := json.Unmarshal(data, &d); err != nil {
		return core.GatewayEvent{}, errors.New("данные вебхука SeverPay не разобраны")
	}
	if d.ID == "" {
		return core.GatewayEvent{}, errors.New("в вебхуке кассы SeverPay нет номера платежа")
	}
	var paymentID int64
	if n, ok := strings.CutPrefix(d.OrderID, orderPrefix); ok {
		paymentID, _ = strconv.ParseInt(n, 10, 64)
	}
	return event(d, string(d.ID), paymentID)
}

func mapStatus(s string) core.PayStatus {
	switch strings.ToLower(s) {
	case "success":
		return core.PayPaid
	case "decline", "fail", "failed", "cancel", "cancelled", "canceled":
		return core.PayFailed
	}
	return core.PayPending
}

// call шлёт подписанный запрос. Токен идёт только в подпись и в ошибки не попадает.
func (g *Gateway) call(ctx context.Context, path string, body map[string]json.RawMessage) (reply, error) {
	var out reply
	mid, ok := g.mid()
	token := g.cfg.Secret(keyToken)
	if !ok || token == "" {
		return out, errors.New("касса SeverPay не настроена")
	}
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return out, errors.New("не получен salt для SeverPay")
	}
	body["mid"] = json.RawMessage(strconv.FormatInt(mid, 10))
	body["salt"] = quote(hex.EncodeToString(salt))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.baseURL+path, strings.NewReader(string(signed(body, token))))
	if err != nil {
		return out, errors.New("не собран запрос к SeverPay")
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := g.client.Do(req)
	if err != nil {
		return out, errors.New("касса SeverPay недоступна")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return out, fmt.Errorf("касса SeverPay ответила кодом %d", resp.StatusCode)
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxBody)).Decode(&out); err != nil {
		return out, errors.New("ответ кассы SeverPay не разобран")
	}
	if !out.Status {
		return out, errors.New("касса SeverPay вернула отказ")
	}
	return out, nil
}
