// Package yookassa это касса ЮKassa (API v3).
// Уведомления ЮKassa не подписаны, поэтому вебхуку не верим: берём из него только номер платежа
// и сами спрашиваем у API, что с платежом. Статус и сумму берём только из ответа API.
package yookassa

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/bini69-oi/worker/core"
	"github.com/bini69-oi/worker/pay"
	"github.com/bini69-oi/worker/settings"
)

// DefaultBaseURL это адрес API ЮKassa.
const DefaultBaseURL = "https://api.yookassa.ru/v3"

func init() {
	settings.AddGateway("yookassa", "Банковская карта и СБП",
		settings.Def{Key: "yookassa.shop_id", Kind: settings.KindString, Default: "", Group: "pay",
			Desc: "Номер магазина в ЮKassa (shopId)"},
		settings.Def{Key: "yookassa.return_url", Kind: settings.KindString, Default: "", Group: "pay",
			Desc: "Куда вернуть абонента после оплаты, {payment} заменится номером платежа. Пусто: обратно в бота"},
	)
}

// Config это настройки, из которых касса на лету читает ключи. Подходит *settings.Settings.
type Config interface {
	String(key string) string
	Secret(key string) string
}

// Gateway это касса ЮKassa.
type Gateway struct {
	cfg     Config
	baseURL string
	client  *http.Client
	nets    []*net.IPNet
	// Return это адрес возврата по умолчанию, если настройка пустая. Ставит сборка: диплинк в бота.
	Return func(paymentID int64) string
}

// New собирает кассу. Пустой baseURL означает боевой API, nil вместо клиента означает клиент с таймаутом.
func New(cfg Config, baseURL string, client *http.Client) *Gateway {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	return &Gateway{cfg: cfg, baseURL: strings.TrimRight(baseURL, "/"), client: client}
}

// RestrictIPs включает необязательный фильтр: вебхуки принимаются только с этих адресов.
// За обратным прокси в RemoteAddr должен приходить настоящий адрес (например через RealIP).
func (g *Gateway) RestrictIPs(nets []*net.IPNet) { g.nets = nets }

// OfficialNets это адреса, с которых ЮKassa шлёт уведомления (по списку из документации).
func OfficialNets() []*net.IPNet {
	var out []*net.IPNet
	for _, s := range []string{"185.71.76.0/27", "185.71.77.0/27", "77.75.153.0/25", "77.75.154.128/25",
		"77.75.156.11/32", "77.75.156.35/32", "2a02:5180::/32"} {
		_, n, _ := net.ParseCIDR(s)
		out = append(out, n)
	}
	return out
}

func (g *Gateway) Name() string { return "yookassa" }

func (g *Gateway) Configured() bool {
	return g.cfg.String("yookassa.shop_id") != "" && g.cfg.Secret("yookassa.secret_key") != ""
}

type apiPayment struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Amount struct {
		Value    string `json:"value"`
		Currency string `json:"currency"`
	} `json:"amount"`
	RefundedAmount struct {
		Value string `json:"value"`
	} `json:"refunded_amount"`
	Confirmation struct {
		URL string `json:"confirmation_url"`
	} `json:"confirmation"`
	Metadata map[string]string `json:"metadata"`
}

// errNotFound это «такого платежа в ЮKassa нет».
var errNotFound = errors.New("платёж не найден в ЮKassa")

// call делает запрос к API. Ключ не попадает ни в ошибки, ни в журнал.
func (g *Gateway) call(ctx context.Context, method, path, idemKey string, body any) (apiPayment, error) {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return apiPayment{}, err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, g.baseURL+path, rd)
	if err != nil {
		return apiPayment{}, errors.New("не собран запрос к ЮKassa")
	}
	req.SetBasicAuth(g.cfg.String("yookassa.shop_id"), g.cfg.Secret("yookassa.secret_key"))
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if idemKey != "" {
		req.Header.Set("Idempotence-Key", idemKey)
	}
	resp, err := g.client.Do(req)
	if err != nil {
		// Текст ошибки сети может содержать адрес, ключей в нём нет, но подробности режем.
		return apiPayment{}, errors.New("ЮKassa недоступна")
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	switch {
	case resp.StatusCode == http.StatusNotFound:
		return apiPayment{}, errNotFound
	case resp.StatusCode == http.StatusUnauthorized:
		return apiPayment{}, errors.New("ЮKassa отклонила ключи магазина")
	case resp.StatusCode/100 != 2:
		return apiPayment{}, fmt.Errorf("ЮKassa ответила кодом %d", resp.StatusCode)
	}
	var p apiPayment
	if err := json.Unmarshal(raw, &p); err != nil {
		return apiPayment{}, errors.New("ответ ЮKassa не разобран")
	}
	return p, nil
}

func (g *Gateway) Create(ctx context.Context, p core.Payment) (core.Invoice, error) {
	body := map[string]any{
		"amount":       map[string]string{"value": formatMoney(p.Amount), "currency": "RUB"},
		"capture":      true,
		"confirmation": map[string]string{"type": "redirect", "return_url": g.returnURL(p.ID)},
		"description":  fmt.Sprintf("Пополнение баланса, платёж %d", p.ID),
		"metadata":     map[string]string{"payment_id": strconv.FormatInt(p.ID, 10)},
	}
	r, err := g.call(ctx, http.MethodPost, "/payments", "worker-"+strconv.FormatInt(p.ID, 10), body)
	if err != nil {
		return core.Invoice{}, err
	}
	if r.ID == "" || r.Confirmation.URL == "" {
		return core.Invoice{}, errors.New("ЮKassa не вернула ссылку на оплату")
	}
	return core.Invoice{ExternalID: r.ID, URL: r.Confirmation.URL, ExtAmount: p.Amount, ExtCurrency: "RUB"}, nil
}

func (g *Gateway) Status(ctx context.Context, p core.Payment) (core.GatewayEvent, error) {
	if p.ExternalID == "" {
		return core.GatewayEvent{}, errors.New("у платежа нет номера в ЮKassa")
	}
	return g.fetch(ctx, p.ExternalID)
}

// fetch спрашивает платёж у API и переводит ответ в событие.
func (g *Gateway) fetch(ctx context.Context, id string) (core.GatewayEvent, error) {
	r, err := g.call(ctx, http.MethodGet, "/payments/"+id, "", nil)
	if err != nil {
		return core.GatewayEvent{}, err
	}
	if r.ID != id {
		return core.GatewayEvent{}, errors.New("ЮKassa вернула другой платёж")
	}
	cents, err := parseMoney(r.Amount.Value)
	if err != nil {
		return core.GatewayEvent{}, err
	}
	if r.Amount.Currency != "RUB" {
		return core.GatewayEvent{}, errors.New("валюта платежа не рубли")
	}
	ev := core.GatewayEvent{ExternalID: r.ID, ExtAmount: cents, Status: core.PayPending}
	switch r.Status {
	case "succeeded":
		ev.Status = core.PayPaid
		// Платёж возвращён целиком: зачисление отменяется. Частичный возврат магазин делает сам
		// и сам решает, что делать с балансом, поэтому оплата остаётся оплатой.
		if back, err := parseMoney(r.RefundedAmount.Value); err == nil && back >= cents {
			ev.Status = core.PayRefunded
		}
	case "canceled":
		ev.Status = core.PayFailed
	}
	ev.PaymentID, _ = strconv.ParseInt(r.Metadata["payment_id"], 10, 64)
	return ev, nil
}

// Verify берёт из уведомления только номер платежа и перепроверяет его через API.
// Платёж, которого API не знает или который не оплачен и не отменён, считается подделкой.
func (g *Gateway) Verify(r *http.Request) (core.GatewayEvent, error) {
	if len(g.nets) > 0 && !g.allowed(r.RemoteAddr) {
		return core.GatewayEvent{}, pay.ErrBadSignature
	}
	var hook struct {
		Event  string `json:"event"`
		Object struct {
			ID        string `json:"id"`
			PaymentID string `json:"payment_id"` // у события возврата
		} `json:"object"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&hook); err != nil {
		return core.GatewayEvent{}, pay.ErrBadSignature
	}
	id := hook.Object.ID
	if hook.Event == "refund.succeeded" {
		id = hook.Object.PaymentID // возврат проверяем по самому платежу: API скажет, сколько возвращено
	} else if !strings.HasPrefix(hook.Event, "payment.") {
		return core.GatewayEvent{}, pay.ErrBadSignature
	}
	if id == "" || strings.ContainsAny(id, "/?#") {
		return core.GatewayEvent{}, pay.ErrBadSignature
	}
	ev, err := g.fetch(r.Context(), id)
	if errors.Is(err, errNotFound) {
		return core.GatewayEvent{}, pay.ErrBadSignature
	}
	if err != nil {
		return core.GatewayEvent{}, err
	}
	if hook.Event == "refund.succeeded" && ev.Status != core.PayRefunded {
		// Частичный возврат: отвечаем «принято», чтобы ЮKassa не повторяла, а зачисление не трогаем.
		return core.GatewayEvent{ExternalID: ev.ExternalID, Status: core.PayPending}, nil
	}
	if ev.Status == core.PayPending {
		return core.GatewayEvent{}, pay.ErrBadSignature
	}
	return ev, nil
}

func (g *Gateway) allowed(remote string) bool {
	host, _, err := net.SplitHostPort(remote)
	if err != nil {
		host = remote
	}
	ip := net.ParseIP(host)
	for _, n := range g.nets {
		if ip != nil && n.Contains(ip) {
			return true
		}
	}
	return false
}

// formatMoney переводит копейки в строку ЮKassa: 12345 станет "123.45".
func formatMoney(cents int64) string {
	return fmt.Sprintf("%d.%02d", cents/100, cents%100)
}

// parseMoney переводит "123.45" в копейки, не трогая дробные числа.
func parseMoney(s string) (int64, error) {
	whole, frac, _ := strings.Cut(s, ".")
	if len(frac) > 2 {
		return 0, errors.New("сумма в ответе ЮKassa не разобрана")
	}
	frac += strings.Repeat("0", 2-len(frac))
	w, err1 := strconv.ParseInt(whole, 10, 64)
	f, err2 := strconv.ParseInt(frac, 10, 64)
	if err1 != nil || err2 != nil || w < 0 || f < 0 {
		return 0, errors.New("сумма в ответе ЮKassa не разобрана")
	}
	return w*100 + f, nil
}

// returnURL это адрес возврата. ЮKassa без него не создаёт платёж, поэтому есть последний запасной адрес.
func (g *Gateway) returnURL(id int64) string {
	if u := pay.ReturnURL(g.cfg.String("yookassa.return_url"), g.Return, id); u != "" {
		return u
	}
	return "https://t.me"
}
