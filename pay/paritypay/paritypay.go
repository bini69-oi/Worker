// Package paritypay это касса ParityPay (API v2): СБП и карты по ссылке, суммы у кассы в рублях, вебхук с подписью HMAC-SHA256.
package paritypay

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
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/bini69-oi/worker/core"
	"github.com/bini69-oi/worker/pay"
	"github.com/bini69-oi/worker/settings"
)

// DefaultBaseURL это адрес API ParityPay.
const DefaultBaseURL = "https://api.paritypay.net"

// orderPrefix метит наш номер платежа в order_id, чтобы узнать его в вебхуке.
const orderPrefix = "worker:"

const (
	keyShop     = "paritypay.shop_id"
	keySecret   = "paritypay.secret_key"
	keyCallback = "paritypay.callback_secret"
	keyMethod   = "paritypay.method"
	keyHook     = "paritypay.hook_url"
	keyReturn   = "paritypay.return_url"
	maxBody     = 1 << 20
)

func init() {
	settings.AddGateway("paritypay", "СБП и карты (ParityPay)",
		settings.Def{Key: keyShop, Kind: settings.KindString, Secret: true, Group: "pay.paritypay",
			Desc: "UUID кассы из кабинета ParityPay (заголовок X-ShopId)"},
		settings.Def{Key: keySecret, Kind: settings.KindString, Secret: true, Group: "pay.paritypay",
			Desc: "Секретный ключ №1 для запросов к API (X-SecretKey)"},
		settings.Def{Key: keyCallback, Kind: settings.KindString, Secret: true, Group: "pay.paritypay",
			Desc: "Секретный ключ №2 для подписи уведомлений (X-SIGNATURE)"},
		settings.Def{Key: keyMethod, Kind: settings.KindString, Group: "pay.paritypay",
			Desc: "Способ оплаты: sbp или card. Пусто: плательщик выберет на странице кассы"},
		settings.Def{Key: keyHook, Kind: settings.KindString, Group: "pay.paritypay",
			Desc: "Адрес вебхука для счетов: https://ваш-сайт/pay/paritypay. Пусто: подставит сборка"},
		settings.Def{Key: keyReturn, Kind: settings.KindString, Group: "pay.paritypay",
			Desc: "Куда вернуть абонента после оплаты, {payment} заменится номером платежа. Пусто: обратно в бота"},
	)
}

// Config это настройки, которые касса читает на лету.
type Config interface {
	String(key string) string
	Secret(key string) string
}

// Gateway это касса ParityPay.
type Gateway struct {
	cfg     Config
	baseURL string
	client  *http.Client
	// Return это адрес возврата по умолчанию, если настройка пустая. Ставит сборка: диплинк в бота.
	Return func(paymentID int64) string
	// Hook это адрес вебхука по умолчанию, если настройка пустая. Ставит сборка: PUBLIC_URL/pay/paritypay.
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

func (g *Gateway) Name() string { return "paritypay" }

// Configured требует все три ключа: без ключа №2 вебхук не проверить, а оплата без вебхука зависнет.
func (g *Gateway) Configured() bool {
	return g.cfg.Secret(keyShop) != "" && g.cfg.Secret(keySecret) != "" && g.cfg.Secret(keyCallback) != ""
}

// invoice это счёт в ответе API. Рубли хранятся строкой, без float.
type invoice struct {
	ID      string `json:"id"`
	OrderID string `json:"order_id"`
	Status  string `json:"status"`
	Link    string `json:"link"`
	Amount  money  `json:"amount"`
}

func (g *Gateway) Create(ctx context.Context, p core.Payment) (core.Invoice, error) {
	req := map[string]any{
		"order_id": orderPrefix + strconv.FormatInt(p.ID, 10),
		"amount":   json.Number(rubles(p.Amount)), // число с двумя знаками, без float
		"comment":  fmt.Sprintf("Пополнение %d", p.ID),
	}
	if m := strings.ToLower(strings.TrimSpace(g.cfg.String(keyMethod))); m == "sbp" || m == "card" {
		req["service"] = m
	}
	if back := pay.ReturnURL(g.cfg.String(keyReturn), g.Return, p.ID); back != "" {
		req["success_url"], req["fail_url"] = back, back
	}
	hook := strings.TrimSpace(g.cfg.String(keyHook))
	if hook == "" {
		hook = g.Hook
	}
	if hook != "" {
		req["callback_url"] = hook
	}
	var out invoice
	if err := g.do(ctx, http.MethodPost, "/v2/invoice/create", req, &out); err != nil {
		return core.Invoice{}, err
	}
	if out.ID == "" || out.Link == "" {
		return core.Invoice{}, errors.New("касса ParityPay не вернула номер счёта или ссылку")
	}
	return core.Invoice{ExternalID: out.ID, URL: out.Link, ExtAmount: p.Amount, ExtCurrency: "RUB"}, nil
}

func (g *Gateway) Status(ctx context.Context, p core.Payment) (core.GatewayEvent, error) {
	remote, err := g.fetch(ctx, p.ExternalID)
	if err != nil {
		return core.GatewayEvent{}, err
	}
	ev := core.GatewayEvent{ExternalID: p.ExternalID, Status: mapStatus(remote.Status)}
	if ev.Status == core.PayPaid {
		ev.ExtAmount, err = kopecks(string(remote.Amount))
	}
	return ev, err
}

func (g *Gateway) fetch(ctx context.Context, id string) (invoice, error) {
	var out invoice
	err := g.do(ctx, http.MethodGet, "/v2/invoice/status?id="+url.QueryEscape(id), nil, &out)
	return out, err
}

// Verify проверяет X-SIGNATURE (HMAC-SHA256 ключом №2 от значений тела в порядке ключей), затем
// переспрашивает API: статус и сумма берутся оттуда. API недоступен: ошибка, ядро получит повтор.
func (g *Gateway) Verify(r *http.Request) (core.GatewayEvent, error) {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxBody))
	if err != nil {
		return core.GatewayEvent{}, errors.New("вебхук кассы ParityPay не прочитан")
	}
	fields, err := parseBody(body)
	if err != nil {
		return core.GatewayEvent{}, pay.ErrBadSignature
	}
	key := g.cfg.Secret(keyCallback)
	got, derr := hex.DecodeString(strings.TrimSpace(r.Header.Get("X-SIGNATURE")))
	if key == "" || derr != nil || !hmac.Equal(got, sign(signPayload(fields), key)) {
		return core.GatewayEvent{}, pay.ErrBadSignature
	}
	var hook invoice
	if err := json.Unmarshal(body, &hook); err != nil {
		return core.GatewayEvent{}, errors.New("вебхук кассы ParityPay не разобран")
	}
	ev := core.GatewayEvent{ExternalID: hook.ID, Status: mapStatus(hook.Status)}
	if n, ok := strings.CutPrefix(hook.OrderID, orderPrefix); ok {
		ev.PaymentID, _ = strconv.ParseInt(n, 10, 64)
	}
	if ev.ExternalID == "" {
		return ev, errors.New("в вебхуке кассы ParityPay нет номера счёта")
	}
	if ev.Status != core.PayPaid && ev.Status != core.PayRefunded {
		return ev, nil
	}
	remote, err := g.fetch(r.Context(), ev.ExternalID)
	if err != nil {
		return ev, err
	}
	if mapStatus(remote.Status) != ev.Status {
		return ev, errors.New("касса ParityPay не подтвердила статус из вебхука")
	}
	if ev.Status == core.PayRefunded {
		return ev, nil
	}
	if ev.ExtAmount, err = kopecks(string(remote.Amount)); err != nil {
		return ev, err
	}
	if n, err := kopecks(string(hook.Amount)); err != nil || n != ev.ExtAmount {
		ev.ExtAmount = n // сумма вебхука не сошлась с API: ядро отклонит зачисление
	}
	return ev, nil
}

func mapStatus(s string) core.PayStatus {
	switch strings.ToUpper(s) {
	case "PAID":
		return core.PayPaid
	case "EXPIRED", "ERROR":
		return core.PayFailed
	case "REFUNDED":
		return core.PayRefunded // возврат после оплаты: ядро отменит зачисление
	}
	return core.PayPending // NEW и всё незнакомое
}

// sign считает подпись уведомления.
func sign(payload, key string) []byte {
	m := hmac.New(sha256.New, []byte(key))
	m.Write([]byte(payload))
	return m.Sum(nil)
}

// signPayload склеивает значения полей без разделителей в порядке ключей.
func signPayload(fields map[string]any) string {
	keys := make([]string, 0, len(fields))
	for k := range fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var sb strings.Builder
	for _, k := range keys {
		sb.WriteString(stringify(fields[k]))
	}
	return sb.String()
}

// stringify переводит значение поля в текст для подписи. Числа остаются ровно как присланы.
func stringify(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case json.Number:
		return x.String()
	case bool:
		return strconv.FormatBool(x)
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
	return strings.TrimSuffix(buf.String(), "\n")
}

// parseBody разбирает тело как объект, сохраняя исходный текст чисел.
func parseBody(body []byte) (map[string]any, error) {
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	var fields map[string]any
	if err := dec.Decode(&fields); err != nil || fields == nil {
		return nil, errors.New("тело не объект")
	}
	return fields, nil
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
		return errors.New("не собран запрос к ParityPay")
	}
	req.Header.Set("X-ShopId", g.cfg.Secret(keyShop))
	req.Header.Set("X-SecretKey", g.cfg.Secret(keySecret))
	req.Header.Set("Content-Type", "application/json")
	resp, err := g.client.Do(req)
	if err != nil {
		return errors.New("касса ParityPay недоступна")
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("касса ParityPay ответила кодом %d", resp.StatusCode)
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxBody)).Decode(out); err != nil {
		return errors.New("ответ кассы ParityPay не разобран")
	}
	return nil
}
