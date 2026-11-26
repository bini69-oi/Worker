// Package cryptomus это касса Cryptomus (крипта). Счёт выставляется в рублях, касса сама пересчитывает его в монету.
// Подпись и запросов, и вебхуков: md5(base64(тело) + платёжный API-ключ).
package cryptomus

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/md5" // md5 здесь формат подписи Cryptomus, а не защита
	"encoding/base64"
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

// DefaultBaseURL это адрес API Cryptomus.
const DefaultBaseURL = "https://api.cryptomus.com/v1"

const (
	keyMerchant = "cryptomus.merchant_id"
	keyAPIKey   = "cryptomus.api_key"
	keyReturn   = "cryptomus.return_url"
	keyCallback = "cryptomus.callback_url"
	maxBody     = 1 << 20
	// orderPrefix метит наши счета. Cryptomus не принимает двоеточие в order_id, поэтому подчёркивание.
	orderPrefix = "worker_"
	lifetimeSec = 3600
)

func init() {
	settings.AddGateway("cryptomus", "Криптовалюта",
		settings.Def{Key: keyMerchant, Kind: settings.KindString, Secret: true, Group: "pay.cryptomus",
			Desc: "Merchant ID из кабинета Cryptomus (Настройки, Merchant UUID)"},
		settings.Def{Key: keyAPIKey, Kind: settings.KindString, Secret: true, Group: "pay.cryptomus",
			Desc: "Платёжный API-ключ (Payment API key) из кабинета Cryptomus"},
		settings.Def{Key: keyReturn, Kind: settings.KindString, Group: "pay.cryptomus",
			Desc: "Куда вернуть абонента после оплаты, {payment} заменится номером платежа. Пусто: обратно в бота"},
		settings.Def{Key: keyCallback, Kind: settings.KindString, Group: "pay.cryptomus",
			Desc: "Адрес вебхука для счетов, https://ваш-домен/pay/cryptomus. Пусто: адрес из кабинета Cryptomus"},
	)
}

// Config это настройки, которые касса читает на лету. Их даёт *settings.Settings.
type Config interface {
	String(key string) string
	Secret(key string) string
}

// Gateway это касса Cryptomus.
type Gateway struct {
	cfg     Config
	baseURL string
	client  *http.Client
	// Return это адрес возврата по умолчанию, если настройка пустая. Ставит сборка: диплинк в бота.
	Return func(paymentID int64) string
	// Hook это адрес вебхука по умолчанию, если настройка пустая. Ставит сборка: PUBLIC_URL/pay/<касса>.
	Hook string
}

var _ pay.Gateway = (*Gateway)(nil)

// New собирает кассу. Пустой baseURL значит DefaultBaseURL, nil вместо клиента значит клиент с таймаутом.
func New(cfg Config, baseURL string, client *http.Client) *Gateway {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	return &Gateway{cfg: cfg, baseURL: strings.TrimRight(baseURL, "/"), client: client}
}

func (g *Gateway) Name() string { return "cryptomus" }

func (g *Gateway) Configured() bool {
	return g.cfg.Secret(keyMerchant) != "" && g.cfg.Secret(keyAPIKey) != ""
}

// signature это md5(base64(тело) + ключ) в hex.
func signature(body []byte, key string) string {
	sum := md5.Sum([]byte(base64.StdEncoding.EncodeToString(body) + key))
	return hex.EncodeToString(sum[:])
}

// payment это счёт в ответе API и в вебхуке (нужные нам поля). В вебхуке статус лежит в status,
// в ответе /payment/info в payment_status.
type payment struct {
	Type     string `json:"type"`
	UUID     string `json:"uuid"`
	OrderID  string `json:"order_id"`
	Amount   amount `json:"amount"`
	Currency string `json:"currency"`
	Status   string `json:"status"`
	PayState string `json:"payment_status"`
	URL      string `json:"url"`
}

// amount это сумма счёта. Cryptomus шлёт строку ("300.00"), число тоже принимаем.
type amount string

func (a *amount) UnmarshalJSON(b []byte) error {
	if len(b) > 0 && b[0] == '"' {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		*a = amount(s)
		return nil
	}
	var n json.Number
	if err := json.Unmarshal(b, &n); err != nil {
		return err
	}
	*a = amount(n.String())
	return nil
}

type apiReply struct {
	State   int             `json:"state"`
	Message string          `json:"message"`
	Result  json.RawMessage `json:"result"`
}

// call зовёт метод API. Ключи уходят только в заголовки и в ошибки не попадают.
func (g *Gateway) call(ctx context.Context, name string, req map[string]any, out any) error {
	merchant, key := g.cfg.Secret(keyMerchant), g.cfg.Secret(keyAPIKey)
	if merchant == "" || key == "" {
		return errors.New("cryptomus: ключи кассы не заданы")
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(req); err != nil {
		return fmt.Errorf("cryptomus: запрос %s не собран", name)
	}
	body := bytes.TrimRight(buf.Bytes(), "\n")
	hr, err := http.NewRequestWithContext(ctx, http.MethodPost, g.baseURL+"/"+name, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("cryptomus: запрос %s не собран", name)
	}
	hr.Header.Set("merchant", merchant)
	hr.Header.Set("sign", signature(body, key))
	hr.Header.Set("Content-Type", "application/json")
	resp, err := g.client.Do(hr)
	if err != nil {
		// Ошибку транспорта не отдаём как есть: в ней может быть адрес запроса.
		return fmt.Errorf("cryptomus: касса не отвечает на %s", name)
	}
	defer resp.Body.Close()
	var reply apiReply
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxBody)).Decode(&reply); err != nil {
		return fmt.Errorf("cryptomus: ответ на %s не разобран (код %d)", name, resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK || reply.State != 0 {
		return fmt.Errorf("cryptomus: касса отказала в %s (код %d): %.100s", name, resp.StatusCode, reply.Message)
	}
	if err := json.Unmarshal(reply.Result, out); err != nil {
		return fmt.Errorf("cryptomus: ответ на %s не разобран", name)
	}
	return nil
}

// Create выставляет счёт в рублях. В order_id кладём наш номер платежа.
func (g *Gateway) Create(ctx context.Context, p core.Payment) (core.Invoice, error) {
	req := map[string]any{
		"amount":   formatKopeks(p.Amount),
		"currency": "RUB",
		"order_id": orderPrefix + strconv.FormatInt(p.ID, 10),
		"lifetime": lifetimeSec,
	}
	if u := pay.ReturnURL(g.cfg.String(keyReturn), g.Return, p.ID); u != "" {
		req["url_return"] = u
	}
	if u := strings.TrimSpace(g.cfg.String(keyCallback)); u != "" {
		req["url_callback"] = u
	} else if g.Hook != "" {
		req["url_callback"] = g.Hook
	}
	var res payment
	if err := g.call(ctx, "payment", req, &res); err != nil {
		return core.Invoice{}, err
	}
	if res.UUID == "" || res.URL == "" {
		return core.Invoice{}, errors.New("cryptomus: касса не вернула счёт или ссылку")
	}
	return core.Invoice{ExternalID: res.UUID, URL: res.URL, ExtAmount: p.Amount, ExtCurrency: "RUB"}, nil
}

// Status спрашивает счёт через /payment/info.
func (g *Gateway) Status(ctx context.Context, p core.Payment) (core.GatewayEvent, error) {
	var res payment
	if err := g.call(ctx, "payment/info", map[string]any{"uuid": p.ExternalID}, &res); err != nil {
		return core.GatewayEvent{}, err
	}
	if res.UUID != p.ExternalID {
		return core.GatewayEvent{}, errors.New("cryptomus: касса ответила про другой счёт")
	}
	return toEvent(res)
}

// toEvent переводит счёт в событие. Таблица статусов:
//
//	paid, paid_over            оплачен: зачисляем сумму счёта (переплату не зачисляем)
//	refund_paid                возврат оплаты: ядро отменяет зачисление
//	cancel                     счёт истёк или отменён
//	fail, system_fail          сбой оплаты
//	wrong_amount и остальные   ждём: недоплату не зачисляем, она может дойти или уйти в возврат
func toEvent(res payment) (core.GatewayEvent, error) {
	ev := core.GatewayEvent{ExternalID: res.UUID, Status: core.PayPending}
	if n, ok := strings.CutPrefix(res.OrderID, orderPrefix); ok {
		ev.PaymentID, _ = strconv.ParseInt(n, 10, 64)
	}
	state := res.Status
	if state == "" {
		state = res.PayState
	}
	switch strings.ToLower(state) {
	case "paid", "paid_over":
		ev.Status = core.PayPaid
	case "refund_paid":
		ev.Status = core.PayRefunded
	case "cancel":
		ev.Status = core.PayExpired
	case "fail", "system_fail":
		ev.Status = core.PayFailed
	}
	if ev.Status != core.PayPaid {
		return ev, nil
	}
	// Оплаченный счёт обязан быть в рублях и с суммой: иначе сравнивать не с чем.
	// Берём сумму счёта (amount), а не payment_amount: тот в монете плательщика.
	if res.Currency != "RUB" {
		return core.GatewayEvent{}, fmt.Errorf("cryptomus: счёт выставлен не в рублях (%q)", res.Currency)
	}
	k, err := pay.Kopecks(string(res.Amount))
	if err != nil {
		return core.GatewayEvent{}, err
	}
	ev.ExtAmount = k
	return ev, nil
}

// Verify проверяет подпись вебхука: поле sign внутри тела, подпись считается по телу без него.
func (g *Gateway) Verify(r *http.Request) (core.GatewayEvent, error) {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxBody))
	if err != nil {
		return core.GatewayEvent{}, errors.New("cryptomus: тело вебхука не прочитано")
	}
	key := g.cfg.Secret(keyAPIKey)
	var head struct {
		Sign string `json:"sign"`
	}
	if err := json.Unmarshal(body, &head); err != nil {
		return core.GatewayEvent{}, errors.New("cryptomus: вебхук не разобран")
	}
	got, decErr := hex.DecodeString(head.Sign)
	want, _ := hex.DecodeString(signature(canonical(body), key))
	if key == "" || decErr != nil || !hmac.Equal(got, want) {
		return core.GatewayEvent{}, pay.ErrBadSignature
	}
	var res payment
	if err := json.Unmarshal(body, &res); err != nil {
		return core.GatewayEvent{}, errors.New("cryptomus: вебхук не разобран")
	}
	if res.Type != "" && res.Type != "payment" {
		// Кошельковые события нам не нужны: ответ 200 без зачисления.
		return core.GatewayEvent{ExternalID: res.UUID, Status: core.PayPending}, nil
	}
	return toEvent(res)
}

// formatKopeks пишет копейки рублями: 30000 в "300.00".
func formatKopeks(k int64) string {
	return fmt.Sprintf("%d.%02d", k/100, k%100)
}
