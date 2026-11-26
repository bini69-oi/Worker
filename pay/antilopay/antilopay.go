// Package antilopay это касса Antilopay (lk.antilopay.com, API v2): СБП и карты по платёжной ссылке.
//
// Запрос подписан закрытым ключом магазина (RSA SHA-256 от точных байт тела, заголовок X-Apay-Sign).
// Вебхук подписан ключом Antilopay и приходит в заголовке X-Apay-Callback. Проверка открытым ключом кассы.
package antilopay

import (
	"bytes"
	"context"
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

// DefaultBaseURL это адрес API Antilopay.
const DefaultBaseURL = "https://lk.antilopay.com/api/v2"

const (
	keyProject = "antilopay.project_id"
	keySecret  = "antilopay.secret_id"
	keyPrivate = "antilopay.private_key"
	keyPublic  = "antilopay.public_key"
	keyMethod  = "antilopay.method"
	keyReturn  = "antilopay.return_url"

	maxBody = 1 << 20
)

var errBadSig = pay.ErrBadSignature

func init() {
	settings.AddGateway("antilopay", "СБП и карты (Antilopay)",
		settings.Def{Key: keyProject, Kind: settings.KindString, Secret: true, Group: "pay.antilopay",
			Desc: "Идентификатор проекта (project identificator) из кабинета Antilopay"},
		settings.Def{Key: keySecret, Kind: settings.KindString, Secret: true, Group: "pay.antilopay",
			Desc: "Secret ID: идентификатор ключа магазина, уходит в заголовке запросов"},
		settings.Def{Key: keyPrivate, Kind: settings.KindString, Secret: true, Group: "pay.antilopay",
			Desc: "Закрытый ключ магазина (RSA, PEM или base64): им подписываются наши запросы"},
		settings.Def{Key: keyPublic, Kind: settings.KindString, Secret: true, Group: "pay.antilopay",
			Desc: "Открытый ключ Antilopay (RSA, PEM): им проверяются вебхуки"},
		settings.Def{Key: keyMethod, Kind: settings.KindString, Group: "pay.antilopay",
			Desc: "Предпочтительный способ: SBP, CARD_RU или SBER_PAY. Пусто: выберет абонент"},
		settings.Def{Key: keyReturn, Kind: settings.KindString, Group: "pay.antilopay",
			Desc: "Куда вернуть абонента после оплаты, {payment} заменится номером платежа. Пусто: обратно в бота"},
	)
}

// Config это настройки, которые касса читает на лету.
type Config interface {
	String(key string) string
	Secret(key string) string
}

// Gateway это касса Antilopay.
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

func (g *Gateway) Name() string { return "antilopay" }

func (g *Gateway) Configured() bool {
	for _, k := range []string{keyProject, keySecret, keyPrivate, keyPublic} {
		if strings.TrimSpace(g.cfg.Secret(k)) == "" {
			return false
		}
	}
	return true
}

// reply это ответ API: code 0 значит успех. Поля платежа лежат на верхнем уровне.
type reply struct {
	Code      int    `json:"code"`
	PaymentID string `json:"payment_id"`
	URL       string `json:"payment_url"`
	Status    string `json:"status"`
	Original  amount `json:"original_amount"`
	Amount    amount `json:"amount"`
}

func (g *Gateway) Create(ctx context.Context, p core.Payment) (core.Invoice, error) {
	in := map[string]any{
		"project_identificator": g.cfg.Secret(keyProject),
		"amount":                json.Number(rubles(p.Amount)),
		"order_id":              strconv.FormatInt(p.ID, 10),
		"currency":              "RUB",
		"product_name":          "Пополнение баланса",
		"product_type":          "services",
		"description":           fmt.Sprintf("Пополнение %d", p.ID),
		"customer":              map[string]string{"email": fmt.Sprintf("u%d@vpn.bot", p.UserID)},
		"merchant_extra":        strconv.FormatInt(p.ID, 10),
	}
	if m := strings.ToUpper(strings.TrimSpace(g.cfg.String(keyMethod))); m != "" {
		in["prefer_methods"] = []string{m}
	}
	if back := pay.ReturnURL(g.cfg.String(keyReturn), g.Return, p.ID); back != "" {
		in["success_url"], in["fail_url"] = back, back
	}
	var res reply
	if err := g.call(ctx, "/payment/create", in, &res); err != nil {
		return core.Invoice{}, err
	}
	if res.PaymentID == "" || res.URL == "" {
		return core.Invoice{}, errors.New("касса Antilopay не вернула номер платежа или ссылку")
	}
	return core.Invoice{ExternalID: res.PaymentID, URL: res.URL, ExtAmount: p.Amount, ExtCurrency: "RUB"}, nil
}

// Status спрашивает кассу о платеже по нашему номеру заказа (POST /payment/check).
func (g *Gateway) Status(ctx context.Context, p core.Payment) (core.GatewayEvent, error) {
	in := map[string]any{
		"project_identificator": g.cfg.Secret(keyProject),
		"order_id":              strconv.FormatInt(p.ID, 10),
	}
	var res reply
	if err := g.call(ctx, "/payment/check", in, &res); err != nil {
		return core.GatewayEvent{}, err
	}
	ev := core.GatewayEvent{PaymentID: p.ID, ExternalID: p.ExternalID, Status: mapStatus(res.Status)}
	if ev.Status != core.PayPaid {
		return ev, nil
	}
	var err error
	ev.ExtAmount, err = paidAmount(res.Original, res.Amount)
	return ev, err
}

// hookBody это поля вебхука Antilopay.
type hookBody struct {
	Type      string `json:"type"`
	PaymentID string `json:"payment_id"`
	OrderID   string `json:"order_id"`
	Status    string `json:"status"`
	Original  amount `json:"original_amount"`
	Amount    amount `json:"amount"`
	Currency  string `json:"currency"`
}

// Verify сверяет подпись из X-Apay-Callback с открытым ключом кассы по точным байтам тела.
// Деньги берутся из подписанного тела: original_amount это сумма до комиссии, её и должен абонент.
func (g *Gateway) Verify(r *http.Request) (core.GatewayEvent, error) {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxBody))
	if err != nil {
		return core.GatewayEvent{}, errors.New("вебхук кассы Antilopay не прочитан")
	}
	if err := checkSignature(g.cfg.Secret(keyPublic), body, r.Header.Get("X-Apay-Callback")); err != nil {
		return core.GatewayEvent{}, err
	}
	var h hookBody
	if err := json.Unmarshal(body, &h); err != nil {
		return core.GatewayEvent{}, errors.New("вебхук кассы Antilopay не разобран")
	}
	id, err := strconv.ParseInt(h.OrderID, 10, 64)
	if err != nil || id <= 0 {
		return core.GatewayEvent{}, errors.New("в вебхуке Antilopay нет нашего номера платежа")
	}
	if h.Currency != "" && !strings.EqualFold(h.Currency, "RUB") {
		return core.GatewayEvent{}, fmt.Errorf("платёж Antilopay не в рублях: %s", h.Currency)
	}
	ev := core.GatewayEvent{PaymentID: id, ExternalID: h.PaymentID, Status: mapStatus(h.Status)}
	if ev.Status == core.PayPaid {
		ev.ExtAmount, err = paidAmount(h.Original, h.Amount)
	}
	return ev, err
}

// mapStatus переводит статус Antilopay: SUCCESS оплачен, закрытые без оплаты это отказ, остальное ждёт.
func mapStatus(s string) core.PayStatus {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case "SUCCESS":
		return core.PayPaid
	case "FAIL", "CANCEL", "CANCELED", "CANCELLED", "EXPIRED", "CHARGEBACK", "REVERSED":
		return core.PayFailed
	}
	return core.PayPending
}

// amount это сумма в рублях: касса шлёт её то числом, то строкой.
type amount string

func (a *amount) UnmarshalJSON(b []byte) error {
	s := strings.TrimSpace(string(b))
	if s == "null" {
		return nil
	}
	*a = amount(strings.Trim(s, `"`))
	return nil
}

// paidAmount берёт original_amount (до комиссии), а без него amount. Без суммы зачислять нечего.
func paidAmount(original, total amount) (int64, error) {
	s := original
	if s == "" {
		s = total
	}
	if s == "" {
		return 0, errors.New("в ответе Antilopay нет суммы")
	}
	return kopecks(string(s))
}

// call шлёт подписанный запрос. Ключи идут только в заголовки и в ошибки не попадают.
func (g *Gateway) call(ctx context.Context, path string, in map[string]any, out *reply) error {
	body, err := json.Marshal(in)
	if err != nil {
		return err
	}
	sig, err := signBody(g.cfg.Secret(keyPrivate), body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return errors.New("не собран запрос к Antilopay")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Apay-Secret-Id", g.cfg.Secret(keySecret))
	req.Header.Set("X-Apay-Sign", sig)
	req.Header.Set("X-Apay-Sign-Version", "1")
	resp, err := g.client.Do(req)
	if err != nil {
		return errors.New("касса Antilopay недоступна")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("касса Antilopay ответила кодом %d", resp.StatusCode)
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxBody)).Decode(out); err != nil {
		return errors.New("ответ кассы Antilopay не разобран")
	}
	if out.Code != 0 {
		return fmt.Errorf("касса Antilopay вернула ошибку, код %d", out.Code)
	}
	return nil
}
