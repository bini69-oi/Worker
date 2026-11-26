// Package jupiter это касса Jupiter (FPGate P2P v2.1): СБП по ссылке или QR, суммы в копейках,
// вебхук с подписью HMAC-SHA256 по списку полей. Оплата по подписи, API нужен для сверки потерянных вебхуков.
package jupiter

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
	"strconv"
	"strings"
	"time"

	"github.com/bini69-oi/worker/core"
	"github.com/bini69-oi/worker/pay"
	"github.com/bini69-oi/worker/settings"
)

// DefaultBaseURL это адрес API Jupiter.
const DefaultBaseURL = "https://app.juppiter.tech"

// orderPrefix метит наш номер платежа в order_id, чтобы узнать его в вебхуке.
const orderPrefix = "worker-"

const (
	keyToken  = "jupiter.token"
	keySecret = "jupiter.secret"
	keyMethod = "jupiter.method_id"
	keyHook   = "jupiter.hook_url"
)

func init() {
	settings.AddGateway("jupiter", "СБП (Jupiter)",
		settings.Def{Key: keyToken, Kind: settings.KindString, Secret: true, Group: "pay.jupiter",
			Desc: "Токен продавца из кабинета Jupiter"},
		settings.Def{Key: keySecret, Kind: settings.KindString, Secret: true, Group: "pay.jupiter",
			Desc: "Секретный ключ из кабинета Jupiter (подпись запросов и вебхуков)"},
		settings.Def{Key: keyMethod, Kind: settings.KindString, Group: "pay.jupiter",
			Desc: "Номер способа оплаты (method_id) из кабинета Jupiter. Пусто: способ по умолчанию"},
		settings.Def{Key: keyHook, Kind: settings.KindString, Group: "pay.jupiter",
			Desc: "Адрес вебхука для счетов: https://ваш-сайт/pay/jupiter. Пусто: подставит сборка"},
	)
}

// Config это настройки, которые касса читает на лету.
type Config interface {
	String(key string) string
	Secret(key string) string
}

// Gateway это касса Jupiter. Адрес возврата ей не нужен: поле redirect в API зарезервировано,
// абонент платит в приложении банка, а статус приходит вебхуком.
type Gateway struct {
	cfg     Config
	baseURL string
	client  *http.Client
	// Hook это адрес вебхука по умолчанию, если настройка пустая. Ставит сборка: PUBLIC_URL/pay/jupiter.
	Hook string
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

func (g *Gateway) Name() string { return "jupiter" }

func (g *Gateway) Configured() bool {
	return g.cfg.Secret(keyToken) != "" && g.cfg.Secret(keySecret) != ""
}

// Ack это ответ на принятый вебхук: Jupiter ждёт код 200, тело как в их примере.
func (g *Gateway) Ack(core.GatewayEvent) string { return `{"status":"ok"}` }

// reply это общий вид ответа API и вебхука. Сумму и recalculated держим как есть: они входят в подпись.
type reply struct {
	Token         string `json:"token"`
	TransactionID any    `json:"transaction_id"`
	OrderID       string `json:"order_id"`
	Amount        struct {
		Value    json.RawMessage `json:"value"`
		Currency string          `json:"currency"`
	} `json:"amount"`
	Recalculated json.RawMessage `json:"recalculated"`
	Status       struct {
		Type string `json:"type"`
		Code string `json:"error_code"`
	} `json:"status"`
	Signature string `json:"signature"`
	Details   struct {
		QR  string `json:"qrcode_url"`
		URL string `json:"url"`
	} `json:"details"`
}

// txID отдаёт номер операции строкой: касса шлёт его и числом, и строкой.
func (r reply) txID() string {
	switch v := r.TransactionID.(type) {
	case string:
		return v
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	}
	return ""
}

// scalar отдаёт значение поля как текст, без кавычек. Пустое и null это пустая строка.
func scalar(raw json.RawMessage) string {
	s := strings.TrimSpace(string(raw))
	if s == "null" {
		return ""
	}
	return strings.Trim(s, `"`)
}

// signString собирает строку для подписи: имя=значение подряд, пустые поля пропускаются.
func signString(parts [][2]string) string {
	var b strings.Builder
	for _, p := range parts {
		if p[1] != "" {
			b.WriteString(p[0] + "=" + p[1])
		}
	}
	return b.String()
}

// sign это hex HMAC-SHA256 от строки подписи с секретным ключом.
func sign(msg, key string) string {
	m := hmac.New(sha256.New, []byte(key))
	m.Write([]byte(msg))
	return hex.EncodeToString(m.Sum(nil))
}

// rubles превращает копейки в строку рублей без float: 12345 это "123.45".
func rubles(kop int64) string { return fmt.Sprintf("%d.%02d", kop/100, kop%100) }

// paymentID достаёт наш номер платежа из order_id.
func paymentID(order string) (int64, error) {
	n, _ := strings.CutPrefix(order, orderPrefix)
	return strconv.ParseInt(n, 10, 64)
}

func mapStatus(s string) core.PayStatus {
	switch strings.ToLower(s) {
	case "success":
		return core.PayPaid
	case "cancelled", "declined", "error":
		return core.PayFailed
	}
	return core.PayPending
}

func (g *Gateway) Create(ctx context.Context, p core.Payment) (core.Invoice, error) {
	order := orderPrefix + strconv.FormatInt(p.ID, 10)
	customer := strconv.FormatInt(p.UserID, 10)
	val := rubles(p.Amount)
	req := map[string]any{
		"token":    g.cfg.Secret(keyToken),
		"order_id": order,
		"amount":   map[string]string{"value": val, "currency": "RUB"},
		"customer": map[string]string{"id": customer, "email": "user@example.com", "phone": "0000000000", "name": "User"},
		"redirect": "false",
		// Описание не подписывается и не содержит ничего, кроме номера платежа.
		"description": fmt.Sprintf("Пополнение %d", p.ID),
	}
	if m := strings.TrimSpace(g.cfg.String(keyMethod)); m != "" {
		req["method_id"] = m
	}
	hook := strings.TrimSpace(g.cfg.String(keyHook))
	if hook == "" {
		hook = g.Hook
	}
	if hook != "" {
		req["callback_url"] = hook
	}
	req["signature"] = sign(signString([][2]string{
		{"token", g.cfg.Secret(keyToken)}, {"order_id", order}, {"amount.value", val}, {"amount.currency", "RUB"},
		{"customer.id", customer}, {"redirect", "false"},
	}), g.cfg.Secret(keySecret))
	res, err := g.do(ctx, "/p2p_payin_v2.1", req)
	if err != nil {
		return core.Invoice{}, err
	}
	if t := strings.ToLower(res.Status.Type); t != "processing" && t != "success" {
		return core.Invoice{}, fmt.Errorf("касса Jupiter отклонила счёт, код %q", res.Status.Code)
	}
	link := res.Details.QR
	if link == "" {
		link = res.Details.URL
	}
	if res.txID() == "" || link == "" {
		return core.Invoice{}, errors.New("касса Jupiter не вернула номер платежа или ссылку")
	}
	return core.Invoice{ExternalID: res.txID(), URL: link, ExtAmount: p.Amount, ExtCurrency: "RUB"}, nil
}

// fetch спрашивает у API текущее состояние платежа.
func (g *Gateway) fetch(ctx context.Context, id string) (reply, error) {
	token := g.cfg.Secret(keyToken)
	return g.do(ctx, "/p2p_status_v2.1", map[string]string{
		"token": token, "transaction_id": id,
		"signature": sign(signString([][2]string{{"token", token}, {"transaction_id", id}}), g.cfg.Secret(keySecret)),
	})
}

// event превращает ответ API в событие. Сумма берётся только у оплаченного платежа.
func event(id string, remote reply) (core.GatewayEvent, error) {
	ev := core.GatewayEvent{ExternalID: id, Status: mapStatus(remote.Status.Type)}
	if ev.Status != core.PayPaid {
		return ev, nil
	}
	var err error
	ev.ExtAmount, err = pay.Kopecks(scalar(remote.Amount.Value))
	return ev, err
}

func (g *Gateway) Status(ctx context.Context, p core.Payment) (core.GatewayEvent, error) {
	remote, err := g.fetch(ctx, p.ExternalID)
	if err != nil {
		return core.GatewayEvent{}, err
	}
	return event(p.ExternalID, remote)
}

// Verify проверяет подпись вебхука и затем переспрашивает API: статус и сумма берутся только оттуда.
// Если API недоступен, возвращается ошибка, и Jupiter повторит вебхук позже.
func (g *Gateway) Verify(r *http.Request) (core.GatewayEvent, error) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		return core.GatewayEvent{}, errors.New("вебхук кассы Jupiter не прочитан")
	}
	var hook reply
	if json.Unmarshal(body, &hook) != nil {
		return core.GatewayEvent{}, pay.ErrBadSignature // тело не JSON: подписи нет
	}
	token, key := g.cfg.Secret(keyToken), g.cfg.Secret(keySecret)
	want := sign(signString([][2]string{
		{"token", token}, {"transaction_id", hook.txID()}, {"order_id", hook.OrderID},
		{"amount.value", scalar(hook.Amount.Value)}, {"amount.currency", hook.Amount.Currency},
		{"recalculated", scalar(hook.Recalculated)}, {"status.type", hook.Status.Type},
	}), key)
	got, err := hex.DecodeString(strings.TrimSpace(hook.Signature))
	wantRaw, _ := hex.DecodeString(want)
	if token == "" || key == "" || err != nil || !hmac.Equal(got, wantRaw) {
		return core.GatewayEvent{}, pay.ErrBadSignature
	}
	if hook.txID() == "" {
		return core.GatewayEvent{}, errors.New("в вебхуке кассы Jupiter нет номера платежа")
	}
	ev := core.GatewayEvent{ExternalID: hook.txID(), Status: mapStatus(hook.Status.Type)}
	pid, _ := paymentID(hook.OrderID)
	ev.PaymentID = pid
	if ev.Status != core.PayPaid {
		return ev, nil
	}
	// Вебхук подписан ключом магазина, поэтому сумма берётся из него. Переспрос API здесь не нужен:
	// вид ответа статуса не сверен с живой кассой, и при расхождении ни одна оплата не зачислилась бы.
	// Status с API остаётся запасной сверкой для потерянных вебхуков.
	n, err := pay.Kopecks(scalar(hook.Amount.Value))
	if err != nil || n <= 0 || !strings.EqualFold(hook.Amount.Currency, "RUB") {
		return core.GatewayEvent{}, errors.New("в вебхуке кассы Jupiter нет суммы в рублях")
	}
	ev.ExtAmount = n
	return ev, nil
}

// do шлёт подписанный запрос в API. Ключи идут только в тело и в ошибки не попадают.
func (g *Gateway) do(ctx context.Context, path string, in any) (reply, error) {
	var out reply
	b, err := json.Marshal(in)
	if err != nil {
		return out, errors.New("не собран запрос к Jupiter")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.baseURL+path, bytes.NewReader(b))
	if err != nil {
		return out, errors.New("не собран запрос к Jupiter")
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := g.client.Do(req)
	if err != nil {
		return out, errors.New("касса Jupiter недоступна")
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return out, fmt.Errorf("касса Jupiter ответила кодом %d", resp.StatusCode)
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&out); err != nil {
		return out, errors.New("ответ кассы Jupiter не разобран")
	}
	return out, nil
}
