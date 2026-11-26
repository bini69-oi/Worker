// Package rollypay это касса RollyPay: оплата по ссылке, вебхук с подписью HMAC-SHA256 и меткой времени.
package rollypay

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
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

// DefaultBaseURL это адрес API RollyPay.
const DefaultBaseURL = "https://rollypay.io/api/v1"

// orderPrefix метит наш номер платежа в order_id, чтобы узнать его в вебхуке.
const orderPrefix = "worker:"

const (
	keyAPI    = "rollypay.api_key"
	keySecret = "rollypay.signing_secret"
	keyMethod = "rollypay.method"
	keyReturn = "rollypay.return_url"
)

func init() {
	settings.AddGateway("rollypay", "RollyPay",
		settings.Def{Key: keyAPI, Kind: settings.KindString, Secret: true, Group: "pay.rollypay",
			Desc: "API-ключ из кабинета RollyPay (заголовок X-API-Key)"},
		settings.Def{Key: keySecret, Kind: settings.KindString, Secret: true, Group: "pay.rollypay",
			Desc: "Секрет подписи вебхуков из кабинета RollyPay"},
		settings.Def{Key: keyMethod, Kind: settings.KindString, Group: "pay.rollypay",
			Desc: "Способ оплаты RollyPay (например sbp). Пусто: выбор на странице кассы"},
		settings.Def{Key: keyReturn, Kind: settings.KindString, Group: "pay.rollypay",
			Desc: "Куда вернуть абонента после оплаты, {payment} заменится номером платежа. Пусто: обратно в бота"},
	)
}

// Config это настройки, которые касса читает на лету.
type Config interface {
	String(key string) string
	Secret(key string) string
}

// Gateway это касса RollyPay.
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

func (g *Gateway) Name() string { return "rollypay" }

func (g *Gateway) Configured() bool {
	return g.cfg.Secret(keyAPI) != "" && g.cfg.Secret(keySecret) != ""
}

type createReq struct {
	Amount          string `json:"amount"`
	Currency        string `json:"payment_currency"`
	OrderID         string `json:"order_id"`
	Method          string `json:"payment_method,omitempty"`
	Description     string `json:"description"`
	RedirectURL     string `json:"redirect_url,omitempty"`
	SuccessRedirect string `json:"success_redirect_url,omitempty"`
	FailRedirect    string `json:"fail_redirect_url,omitempty"`
}

// apiPay это общие поля ответа API и вебхука.
type apiPay struct {
	PaymentID string      `json:"payment_id"`
	OrderID   string      `json:"order_id"`
	PayURL    string      `json:"pay_url"`
	Status    string      `json:"status"`
	Amount    json.Number `json:"amount"`
	Currency  string      `json:"currency"`
}

// rubAmount отдаёт сумму из ответа API: сумма обязана быть, валюта, если указана, только рубли.
func (a apiPay) rubAmount() (int64, error) {
	if a.Currency != "" && !strings.EqualFold(a.Currency, "RUB") {
		return 0, fmt.Errorf("платёж RollyPay не в рублях: %s", a.Currency)
	}
	if a.Amount == "" {
		return 0, errors.New("в ответе RollyPay нет суммы")
	}
	return kopecks(string(a.Amount))
}

func (g *Gateway) Create(ctx context.Context, p core.Payment) (core.Invoice, error) {
	back := pay.ReturnURL(g.cfg.String(keyReturn), g.Return, p.ID)
	req := createReq{
		Amount:          rubles(p.Amount),
		Currency:        "RUB",
		OrderID:         orderPrefix + strconv.FormatInt(p.ID, 10),
		Method:          g.cfg.String(keyMethod),
		Description:     fmt.Sprintf("Пополнение %d", p.ID),
		RedirectURL:     back,
		SuccessRedirect: back,
		FailRedirect:    back,
	}
	var out apiPay
	if err := g.do(ctx, http.MethodPost, "/payments", req, &out); err != nil {
		return core.Invoice{}, err
	}
	if out.PaymentID == "" || out.PayURL == "" {
		return core.Invoice{}, errors.New("касса RollyPay не вернула номер платежа или ссылку")
	}
	return core.Invoice{ExternalID: out.PaymentID, URL: out.PayURL, ExtAmount: p.Amount, ExtCurrency: "RUB"}, nil
}

func (g *Gateway) Status(ctx context.Context, p core.Payment) (core.GatewayEvent, error) {
	remote, err := g.fetch(ctx, p.ExternalID)
	if err != nil {
		return core.GatewayEvent{}, err
	}
	ev := core.GatewayEvent{ExternalID: p.ExternalID, Status: mapStatus(remote.Status)}
	if ev.Status == core.PayPaid {
		ev.ExtAmount, err = remote.rubAmount()
	}
	return ev, err
}

func (g *Gateway) fetch(ctx context.Context, id string) (apiPay, error) {
	var out apiPay
	err := g.do(ctx, http.MethodGet, "/payments/"+url.PathEscape(id), nil, &out)
	return out, err
}

// Verify проверяет подпись вебхука: X-Signature это hex HMAC-SHA256 от "<X-Timestamp>.<тело>" с секретом подписи.
// Оплату и возврат затем подтверждает API: статус и сумма берутся оттуда.
func (g *Gateway) Verify(r *http.Request) (core.GatewayEvent, error) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		return core.GatewayEvent{}, errors.New("вебхук кассы RollyPay не прочитан")
	}
	secret := g.cfg.Secret(keySecret)
	got, err := hex.DecodeString(strings.TrimSpace(r.Header.Get("X-Signature")))
	if secret == "" || err != nil || !hmac.Equal(got, sign(r.Header.Get("X-Timestamp"), body, secret)) {
		return core.GatewayEvent{}, pay.ErrBadSignature
	}
	var hook apiPay
	if err := json.Unmarshal(body, &hook); err != nil {
		return core.GatewayEvent{}, errors.New("вебхук кассы RollyPay не разобран")
	}
	ev := core.GatewayEvent{ExternalID: hook.PaymentID, Status: mapStatus(hook.Status)}
	if n, ok := strings.CutPrefix(hook.OrderID, orderPrefix); ok {
		ev.PaymentID, _ = strconv.ParseInt(n, 10, 64)
	}
	if ev.ExternalID == "" {
		return ev, errors.New("в вебхуке кассы RollyPay нет номера платежа")
	}
	hookAmount, _ := kopecks(string(hook.Amount))
	if ev.Status != core.PayPaid && ev.Status != core.PayRefunded {
		return ev, nil
	}
	remote, err := g.fetch(r.Context(), ev.ExternalID)
	if err != nil {
		return ev, err
	}
	if mapStatus(remote.Status) != ev.Status {
		return ev, errors.New("касса RollyPay не подтвердила статус из вебхука")
	}
	if ev.Status == core.PayRefunded {
		return ev, nil
	}
	if ev.ExtAmount, err = remote.rubAmount(); err != nil {
		return ev, err
	}
	if hookAmount != ev.ExtAmount {
		ev.ExtAmount = hookAmount // сумма вебхука не сошлась с API: ядро отклонит зачисление
	}
	return ev, nil
}

// sign считает подпись вебхука.
func sign(timestamp string, body []byte, secret string) []byte {
	m := hmac.New(sha256.New, []byte(secret))
	m.Write([]byte(timestamp + "."))
	m.Write(body)
	return m.Sum(nil)
}

func mapStatus(s string) core.PayStatus {
	switch strings.ToLower(s) {
	case "paid":
		return core.PayPaid
	case "expired", "canceled", "cancelled":
		return core.PayFailed
	case "chargeback":
		return core.PayRefunded // возврат после оплаты: ядро отменит зачисление
	}
	return core.PayPending
}

// do шлёт запрос в API. Ключ идёт только в заголовок и в ошибки не попадает.
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
		return errors.New("не собран запрос к RollyPay")
	}
	nonce := make([]byte, 16)
	_, _ = rand.Read(nonce)
	req.Header.Set("X-API-Key", g.cfg.Secret(keyAPI))
	req.Header.Set("X-Nonce", hex.EncodeToString(nonce))
	req.Header.Set("Content-Type", "application/json")
	resp, err := g.client.Do(req)
	if err != nil {
		return errors.New("касса RollyPay недоступна")
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("касса RollyPay ответила кодом %d", resp.StatusCode)
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(out); err != nil {
		return errors.New("ответ кассы RollyPay не разобран")
	}
	return nil
}
