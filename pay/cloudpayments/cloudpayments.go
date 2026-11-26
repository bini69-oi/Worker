// Package cloudpayments это касса CloudPayments: оплата картой по ссылке на счёт (orders/create).
// Уведомления Pay, Fail и Refund приходят формой, подпись Content-HMAC считается по точным байтам тела.
// Рекуррентные списания не поддерживаются.
package cloudpayments

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
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

// DefaultBaseURL это адрес API CloudPayments.
const DefaultBaseURL = "https://api.cloudpayments.ru"

const (
	keyTest     = "cloudpayments.test"
	keyPublicID = "cloudpayments.public_id"
	keySecret   = "cloudpayments.api_secret"
	keyReturn   = "cloudpayments.return_url"
	maxBody     = 1 << 20
)

func init() {
	settings.AddGateway("cloudpayments", "Карты (CloudPayments)",
		settings.Def{Key: keyPublicID, Kind: settings.KindString, Secret: true, Group: "pay.cloudpayments",
			Desc: "Public ID сайта из кабинета CloudPayments"},
		settings.Def{Key: keySecret, Kind: settings.KindString, Secret: true, Group: "pay.cloudpayments",
			Desc: "Пароль для API из кабинета CloudPayments. В кабинете укажите адрес https://ваш-сайт/pay/cloudpayments для уведомлений Pay, Fail и Refund"},
		settings.Def{Key: keyReturn, Kind: settings.KindString, Group: "pay.cloudpayments",
			Desc: "Куда вернуть абонента после оплаты, {payment} заменится номером платежа. Пусто: обратно в бота"},
		settings.Def{Key: keyTest, Kind: settings.KindBool, Default: "false", Group: "pay.cloudpayments",
			Desc: "Зачислять тестовые платежи (TestMode). Только для тестового стенда: тестовая карта платит бесплатно"},
	)
}

// Config это настройки, которые касса читает на лету.
type Config interface {
	String(key string) string
	Int(key string) int64
	Secret(key string) string
}

// Gateway это касса CloudPayments. Адрес уведомлений задаётся в кабинете кассы, а не в запросе,
// поэтому поля Hook нет.
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

func (g *Gateway) Name() string { return "cloudpayments" }

func (g *Gateway) Configured() bool {
	return g.cfg.Secret(keyPublicID) != "" && g.cfg.Secret(keySecret) != ""
}

// Ack это ответ на принятое уведомление: без {"code":0} касса повторяет его.
func (g *Gateway) Ack(core.GatewayEvent) string { return `{"code":0}` }

// reply это общая обёртка ответов API.
type reply struct {
	Success bool            `json:"Success"`
	Message string          `json:"Message"`
	Model   json.RawMessage `json:"Model"`
}

// Create выставляет счёт по ссылке. Наш номер платежа идёт в InvoiceId и он же служит номером в кассе.
func (g *Gateway) Create(ctx context.Context, p core.Payment) (core.Invoice, error) {
	id := strconv.FormatInt(p.ID, 10)
	req := map[string]any{
		"Amount":      json.Number(pay.Rubles(p.Amount)),
		"Currency":    "RUB",
		"Description": fmt.Sprintf("Пополнение %d", p.ID),
		"InvoiceId":   id,
		"AccountId":   strconv.FormatInt(p.UserID, 10),
	}
	if back := pay.ReturnURL(g.cfg.String(keyReturn), g.Return, p.ID); back != "" {
		req["SuccessRedirectUrl"], req["FailRedirectUrl"] = back, back
	}
	var model struct {
		URL string `json:"Url"`
	}
	if _, err := g.call(ctx, "orders/create", req, &model); err != nil {
		return core.Invoice{}, err
	}
	if model.URL == "" {
		return core.Invoice{}, errors.New("касса CloudPayments не вернула ссылку на счёт")
	}
	return core.Invoice{ExternalID: id, URL: model.URL, ExtAmount: p.Amount, ExtCurrency: "RUB"}, nil
}

// tx это платёж в ответе payments/find.
type tx struct {
	Amount    json.Number `json:"Amount"`
	Currency  string      `json:"Currency"`
	Status    string      `json:"Status"`
	InvoiceID string      `json:"InvoiceId"`
}

// Status ищет платёж по InvoiceId. Платежа ещё нет: ждём дальше.
func (g *Gateway) Status(ctx context.Context, p core.Payment) (core.GatewayEvent, error) {
	id := strconv.FormatInt(p.ID, 10)
	var t tx
	found, err := g.call(ctx, "payments/find", map[string]any{"InvoiceId": id}, &t)
	ev := core.GatewayEvent{PaymentID: p.ID, ExternalID: id, Status: core.PayPending}
	if err != nil || !found {
		return ev, err
	}
	if t.InvoiceID != "" && t.InvoiceID != id {
		return core.GatewayEvent{}, errors.New("касса CloudPayments ответила про другой счёт")
	}
	ev.Status = mapStatus(t.Status)
	if ev.Status != core.PayPaid {
		return ev, nil
	}
	ev.ExtAmount, err = rubAmount(t.Amount, t.Currency)
	return ev, err
}

// mapStatus переводит статус платежа. Authorized это только холд без списания, его не зачисляем.
func mapStatus(s string) core.PayStatus {
	switch strings.ToLower(s) {
	case "completed":
		return core.PayPaid
	case "declined", "cancelled", "canceled":
		return core.PayFailed
	}
	return core.PayPending
}

// rubAmount переводит сумму в копейки, валюта обязана быть рублями (пустая допускается).
func rubAmount(n json.Number, cur string) (int64, error) {
	if cur != "" && !strings.EqualFold(cur, "RUB") {
		return 0, fmt.Errorf("платёж CloudPayments не в рублях: %s", cur)
	}
	return pay.Kopecks(string(n))
}

// Verify проверяет подпись по сырому телу и разбирает форму уведомления.
func (g *Gateway) Verify(r *http.Request) (core.GatewayEvent, error) {
	raw, err := io.ReadAll(io.LimitReader(r.Body, maxBody))
	if err != nil {
		return core.GatewayEvent{}, errors.New("тело уведомления CloudPayments не прочитано")
	}
	sig := r.Header.Get("Content-HMAC")
	if sig == "" {
		sig = r.Header.Get("X-Content-HMAC")
	}
	if !validSignature(raw, sig, g.cfg.Secret(keySecret)) {
		return core.GatewayEvent{}, pay.ErrBadSignature
	}
	f, err := url.ParseQuery(string(raw))
	if err != nil {
		return core.GatewayEvent{}, errors.New("уведомление CloudPayments не разобрано")
	}
	n, err := strconv.ParseInt(f.Get("InvoiceId"), 10, 64)
	if err != nil || n <= 0 {
		return core.GatewayEvent{}, errors.New("в уведомлении CloudPayments нет нашего номера платежа")
	}
	ev := core.GatewayEvent{PaymentID: n, ExternalID: strconv.FormatInt(n, 10), Status: core.PayPending}
	// Сайт в тестовом режиме шлёт настоящие подписанные уведомления по тестовым картам: денег за ними нет.
	if f.Get("TestMode") == "1" && g.cfg.String(keyTest) != "true" {
		return ev, nil
	}
	switch {
	case strings.EqualFold(f.Get("OperationType"), "Refund") || f.Get("PaymentTransactionId") != "":
		ev.Status = core.PayRefunded
	case f.Get("ReasonCode") != "" || f.Get("Reason") != "":
		ev.Status = core.PayFailed
	default:
		ev.Status = mapStatus(f.Get("Status"))
	}
	if ev.Status == core.PayPaid || ev.Status == core.PayRefunded {
		ev.ExtAmount, err = rubAmount(json.Number(f.Get("Amount")), f.Get("Currency"))
	}
	return ev, err
}

// sign это base64(HMAC-SHA256(тело, секрет)).
func sign(body []byte, secret string) string {
	m := hmac.New(sha256.New, []byte(secret))
	m.Write(body)
	return base64.StdEncoding.EncodeToString(m.Sum(nil))
}

// validSignature сравнивает подписи за постоянное время. Пустой секрет не подписывает ничего.
func validSignature(body []byte, got, secret string) bool {
	if secret == "" || got == "" {
		return false
	}
	return hmac.Equal([]byte(sign(body, secret)), []byte(strings.TrimSpace(got)))
}

// call зовёт метод API. Второе значение: платёж найден (для payments/find). Ключи идут только в заголовок
// Basic и в ошибки не попадают.
func (g *Gateway) call(ctx context.Context, name string, in map[string]any, model any) (bool, error) {
	id, secret := g.cfg.Secret(keyPublicID), g.cfg.Secret(keySecret)
	if id == "" || secret == "" {
		return false, errors.New("касса CloudPayments не настроена")
	}
	body, err := json.Marshal(in)
	if err != nil {
		return false, fmt.Errorf("запрос CloudPayments %s не собран", name)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.baseURL+"/"+name, bytes.NewReader(body))
	if err != nil {
		return false, fmt.Errorf("запрос CloudPayments %s не собран", name)
	}
	req.SetBasicAuth(id, secret)
	req.Header.Set("Content-Type", "application/json")
	resp, err := g.client.Do(req)
	if err != nil {
		// Ошибку транспорта не отдаём как есть: в ней может быть адрес запроса.
		return false, fmt.Errorf("касса CloudPayments не отвечает на %s", name)
	}
	defer resp.Body.Close()
	var out reply
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxBody)).Decode(&out); err != nil || resp.StatusCode >= 300 {
		return false, fmt.Errorf("касса CloudPayments отказала в %s (код %d)", name, resp.StatusCode)
	}
	if !out.Success {
		if strings.Contains(strings.ToLower(out.Message), "not found") {
			return false, nil
		}
		return false, fmt.Errorf("касса CloudPayments отказала в %s", name)
	}
	if err := json.Unmarshal(out.Model, model); err != nil {
		return false, fmt.Errorf("ответ CloudPayments на %s не разобран", name)
	}
	return true, nil
}
