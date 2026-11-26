// Package tbank это касса Т-Банка (интернет-эквайринг, API v2, бывший Тинькофф).
//
// Счёт создаёт Init (TerminalKey, Amount в копейках, OrderId = наш номер платежа), в ответе PaymentId и PaymentURL.
// Подпись Token: SHA-256 от склейки значений корневых скалярных полей и Password, отсортированных по имени поля.
// Вложенные объекты и массивы, а также сам Token не входят. Уведомление приходит JSON-ом на NotificationURL
// с тем же правилом, ответить на него нужно текстом OK. Статус платежа отдаёт GetState.
package tbank

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/bini69-oi/worker/core"
	"github.com/bini69-oi/worker/pay"
	"github.com/bini69-oi/worker/settings"
)

// DefaultBaseURL это боевой адрес API v2.
const DefaultBaseURL = "https://securepay.tinkoff.ru/v2"

const (
	keyTerminal = "tbank.terminal_key"
	keyPassword = "tbank.password"
	keyReturn   = "tbank.return_url"
	keyHook     = "tbank.hook_url"

	maxBody = 1 << 20
)

func init() {
	settings.AddGateway("tbank", "Карта и СБП (Т-Банк)",
		settings.Def{Key: keyTerminal, Kind: settings.KindString, Group: "pay.tbank", Secret: true,
			Desc: "Ключ терминала (TerminalKey) из кабинета Т-Банка"},
		settings.Def{Key: keyPassword, Kind: settings.KindString, Group: "pay.tbank", Secret: true,
			Desc: "Пароль терминала: им подписываются запросы и уведомления"},
		settings.Def{Key: keyHook, Kind: settings.KindString, Group: "pay.tbank",
			Desc: "Адрес уведомлений: https://ваш-сайт/pay/tbank. Пусто: подставит сборка"},
		settings.Def{Key: keyReturn, Kind: settings.KindString, Group: "pay.tbank",
			Desc: "Куда вернуть абонента после оплаты, {payment} заменится номером платежа. Пусто: обратно в бота"},
	)
}

// Config это настройки, которые касса читает на лету.
type Config interface {
	String(key string) string
	Secret(key string) string
}

// Gateway это касса Т-Банка.
type Gateway struct {
	cfg     Config
	baseURL string
	client  *http.Client
	// Return это адрес возврата по умолчанию, если настройка пустая. Ставит сборка: диплинк в бота.
	Return func(paymentID int64) string
	// Hook это адрес уведомлений по умолчанию, если настройка пустая. Ставит сборка: PUBLIC_URL/pay/tbank.
	Hook string
}

var (
	_ pay.Gateway = (*Gateway)(nil)
	_ pay.Acker   = (*Gateway)(nil)
)

// New собирает кассу. Пустой baseURL означает боевой адрес, nil вместо клиента означает клиент с таймаутом.
func New(cfg Config, baseURL string, client *http.Client) *Gateway {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	return &Gateway{cfg: cfg, baseURL: strings.TrimRight(baseURL, "/"), client: client}
}

func (g *Gateway) Name() string { return "tbank" }

func (g *Gateway) Configured() bool {
	return g.cfg.Secret(keyTerminal) != "" && g.cfg.Secret(keyPassword) != ""
}

// scalar переводит корневое значение в строку для подписи. Вложенное и пустое в подпись не входит.
// Булевы значения идут как "true" и "false", числа как в JSON.
func scalar(v any) (string, bool) {
	switch x := v.(type) {
	case string:
		return x, true
	case json.Number:
		return x.String(), true
	case int64:
		return strconv.FormatInt(x, 10), true
	case bool:
		return strconv.FormatBool(x), true
	}
	return "", false
}

// token считает подпись: значения скалярных полей (без Token) и Password по порядку имён, SHA-256 в hex.
func token(fields map[string]any, password string) string {
	vals := map[string]string{"Password": password}
	for k, v := range fields {
		if s, ok := scalar(v); ok && k != "Token" {
			vals[k] = s
		}
	}
	keys := make([]string, 0, len(vals))
	for k := range vals {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		b.WriteString(vals[k])
	}
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}

// reply это ответ Init и GetState.
type reply struct {
	Success    bool        `json:"Success"`
	ErrorCode  string      `json:"ErrorCode"`
	Status     string      `json:"Status"`
	PaymentID  json.Number `json:"PaymentId"`
	OrderID    string      `json:"OrderId"`
	Amount     int64       `json:"Amount"`
	PaymentURL string      `json:"PaymentURL"`
}

func (g *Gateway) Create(ctx context.Context, p core.Payment) (core.Invoice, error) {
	term, pass := g.cfg.Secret(keyTerminal), g.cfg.Secret(keyPassword)
	if term == "" || pass == "" {
		return core.Invoice{}, errors.New("касса Т-Банка не настроена")
	}
	req := map[string]any{
		"TerminalKey": term,
		"Amount":      p.Amount,
		"OrderId":     strconv.FormatInt(p.ID, 10),
		"Description": fmt.Sprintf("Пополнение %d", p.ID),
	}
	hook := strings.TrimSpace(g.cfg.String(keyHook))
	if hook == "" {
		hook = g.Hook
	}
	if hook != "" {
		req["NotificationURL"] = hook
	}
	if back := pay.ReturnURL(g.cfg.String(keyReturn), g.Return, p.ID); back != "" {
		req["SuccessURL"], req["FailURL"] = back, back
	}
	var res reply
	if err := g.call(ctx, "/Init", req, pass, &res); err != nil {
		return core.Invoice{}, err
	}
	if res.PaymentID == "" || res.PaymentURL == "" {
		return core.Invoice{}, errors.New("касса Т-Банка не вернула номер платежа или ссылку")
	}
	return core.Invoice{ExternalID: res.PaymentID.String(), URL: res.PaymentURL, ExtAmount: p.Amount, ExtCurrency: "RUB"}, nil
}

// call подписывает запрос, отправляет и разбирает ответ. Текст сетевых ошибок не отдаём: в нём бывает адрес с ключом.
func (g *Gateway) call(ctx context.Context, path string, req map[string]any, pass string, out *reply) error {
	req["Token"] = token(req, pass)
	body, err := json.Marshal(req)
	if err != nil {
		return errors.New("не собран запрос к Т-Банку")
	}
	hr, err := http.NewRequestWithContext(ctx, http.MethodPost, g.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return errors.New("не собран запрос к Т-Банку")
	}
	hr.Header.Set("Content-Type", "application/json")
	resp, err := g.client.Do(hr)
	if err != nil {
		return errors.New("касса Т-Банка недоступна")
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("касса Т-Банка ответила кодом %d", resp.StatusCode)
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return errors.New("ответ Т-Банка не разобран")
	}
	if !out.Success || out.ErrorCode != "0" {
		return fmt.Errorf("касса Т-Банка отказала, код %s", out.ErrorCode)
	}
	return nil
}

// Status спрашивает платёж методом GetState.
func (g *Gateway) Status(ctx context.Context, p core.Payment) (core.GatewayEvent, error) {
	term, pass := g.cfg.Secret(keyTerminal), g.cfg.Secret(keyPassword)
	if term == "" || pass == "" || p.ExternalID == "" {
		return core.GatewayEvent{}, errors.New("нет ключей или номера платежа для запроса к Т-Банку")
	}
	var res reply
	req := map[string]any{"TerminalKey": term, "PaymentId": json.Number(p.ExternalID)}
	if err := g.call(ctx, "/GetState", req, pass, &res); err != nil {
		return core.GatewayEvent{}, err
	}
	return event(res, p.ID, p.ExternalID), nil
}

// event переводит ответ кассы в событие ядра. Деньги зачисляются только по CONFIRMED.
// AUTHORIZED это холд двухстадийной оплаты, зачислять его рано.
// PARTIAL_REFUNDED остаётся «ожидает»: ядро умеет только полный возврат и снимет с баланса всю сумму,
// а часть клиенту вернули. Частичный возврат админ разбирает руками в кабинете Т-Банка.
func event(r reply, id int64, ext string) core.GatewayEvent {
	ev := core.GatewayEvent{PaymentID: id, ExternalID: ext, Status: core.PayPending}
	switch r.Status {
	case "CONFIRMED":
		ev.Status, ev.ExtAmount = core.PayPaid, r.Amount
	case "REJECTED", "CANCELED", "DEADLINE_EXPIRED", "AUTH_FAIL":
		ev.Status = core.PayFailed
	case "REFUNDED", "REVERSED":
		ev.Status, ev.ExtAmount = core.PayRefunded, r.Amount
	}
	return ev
}

// Verify проверяет уведомление: Token считается по всем скалярным полям, что пришли, подпись сравнивается за постоянное время.
func (g *Gateway) Verify(r *http.Request) (core.GatewayEvent, error) {
	term, pass := g.cfg.Secret(keyTerminal), g.cfg.Secret(keyPassword)
	if term == "" || pass == "" {
		return core.GatewayEvent{}, pay.ErrBadSignature
	}
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, maxBody))
	dec.UseNumber() // числа остаются в том виде, как их подписала касса
	var fields map[string]any
	if err := dec.Decode(&fields); err != nil {
		return core.GatewayEvent{}, pay.ErrBadSignature
	}
	got, _ := fields["Token"].(string)
	want := token(fields, pass)
	if subtle.ConstantTimeCompare([]byte(strings.ToLower(got)), []byte(want)) != 1 {
		return core.GatewayEvent{}, pay.ErrBadSignature
	}
	if t, _ := fields["TerminalKey"].(string); t != term {
		return core.GatewayEvent{}, pay.ErrBadSignature
	}
	order, _ := fields["OrderId"].(string)
	id, err := strconv.ParseInt(order, 10, 64)
	if err != nil || id <= 0 {
		return core.GatewayEvent{}, pay.ErrBadSignature
	}
	pid, _ := fields["PaymentId"].(json.Number)
	amount, _ := fields["Amount"].(json.Number)
	cents, err := amount.Int64()
	if err != nil || pid == "" {
		return core.GatewayEvent{}, pay.ErrBadSignature
	}
	status, _ := fields["Status"].(string)
	return event(reply{Status: status, Amount: cents}, id, pid.String()), nil
}

// Ack это ответ на уведомление: без текста OK касса шлёт его повторно.
func (g *Gateway) Ack(core.GatewayEvent) string { return "OK" }
