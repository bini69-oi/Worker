// Package tabpay это касса TabPay: СБП и карта по ссылке, суммы в копейках,
// вебхук с подписью X-Signature-V2 (HMAC-SHA256 от "метка.тело") и проверкой по API.
package tabpay

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
	"strconv"
	"strings"
	"time"

	"github.com/bini69-oi/worker/core"
	"github.com/bini69-oi/worker/pay"
	"github.com/bini69-oi/worker/settings"
)

// DefaultBaseURL это адрес API TabPay.
const DefaultBaseURL = "https://tabpay.org/api"

// orderPrefix метит наш номер платежа в orderId, чтобы узнать его в вебхуке.
const orderPrefix = "worker-"

// maxSkew это допустимый разброс метки времени вебхука: так перехваченный вебхук не переиграть.
const maxSkew = 5 * time.Minute

const (
	keyAPI    = "tabpay.api_key"
	keySecret = "tabpay.webhook_secret"
	keyMethod = "tabpay.method"
	keyReturn = "tabpay.return_url"
)

func init() {
	settings.AddGateway("tabpay", "TabPay",
		settings.Def{Key: keyAPI, Kind: settings.KindString, Secret: true, Group: "pay.tabpay",
			Desc: "API-ключ магазина из кабинета TabPay (tp_..., заголовок X-Api-Key)"},
		settings.Def{Key: keySecret, Kind: settings.KindString, Secret: true, Group: "pay.tabpay",
			Desc: "Секрет подписи из вкладки Webhook кабинета TabPay"},
		settings.Def{Key: keyMethod, Kind: settings.KindString, Group: "pay.tabpay",
			Desc: "Способ оплаты: CARD или SBP. Пусто: абонент выбирает на странице кассы"},
		settings.Def{Key: keyReturn, Kind: settings.KindString, Group: "pay.tabpay",
			Desc: "Куда вернуть абонента после оплаты, {payment} заменится номером платежа. Пусто: обратно в бота"},
	)
}

// Config это настройки, которые касса читает на лету.
type Config interface {
	String(key string) string
	Secret(key string) string
}

// Gateway это касса TabPay. Адрес вебхука (POST /pay/tabpay) вписывается в кабинете кассы.
type Gateway struct {
	cfg     Config
	baseURL string
	client  *http.Client
	now     func() time.Time
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
	return &Gateway{cfg: cfg, baseURL: strings.TrimRight(baseURL, "/"), client: client, now: time.Now}
}

func (g *Gateway) Name() string { return "tabpay" }

func (g *Gateway) Configured() bool {
	return g.cfg.Secret(keyAPI) != "" && g.cfg.Secret(keySecret) != ""
}

type createReq struct {
	OrderID string `json:"orderId"`
	Amount  int64  `json:"amountKopecks"`
	Desc    string `json:"description"`
	Method  string `json:"method,omitempty"`
	Success string `json:"successUrl,omitempty"`
	Fail    string `json:"failUrl,omitempty"`
}

// apiPay это общие поля ответа API и вебхука. Сумма только целыми копейками.
type apiPay struct {
	ID      string      `json:"id"`
	OrderID string      `json:"orderId"`
	Status  string      `json:"status"`
	Amount  json.Number `json:"amountKopecks"`
	PayURL  string      `json:"payUrl"`
	Test    bool        `json:"test"`   // в вебхуке
	IsTest  bool        `json:"isTest"` // в объекте платежа
}

// kopecks отдаёт сумму: целое положительное число, всё остальное это ошибка.
func (a apiPay) kopecks() (int64, error) {
	n, err := strconv.ParseInt(string(a.Amount), 10, 64)
	if err != nil || n <= 0 {
		return 0, errors.New("в ответе TabPay нет суммы в копейках")
	}
	return n, nil
}

func (g *Gateway) Create(ctx context.Context, p core.Payment) (core.Invoice, error) {
	back := pay.ReturnURL(g.cfg.String(keyReturn), g.Return, p.ID)
	req := createReq{
		OrderID: orderPrefix + strconv.FormatInt(p.ID, 10),
		Amount:  p.Amount,
		Desc:    fmt.Sprintf("Пополнение %d", p.ID),
		Success: back,
		Fail:    back,
	}
	if m := strings.ToUpper(strings.TrimSpace(g.cfg.String(keyMethod))); m == "CARD" || m == "SBP" {
		req.Method = m
	}
	var out apiPay
	if err := g.do(ctx, http.MethodPost, "/v1/payments", req, &out); err != nil {
		return core.Invoice{}, err
	}
	if out.ID == "" || out.PayURL == "" {
		return core.Invoice{}, errors.New("касса TabPay не вернула номер платежа или ссылку")
	}
	return core.Invoice{ExternalID: out.ID, URL: out.PayURL, ExtAmount: p.Amount, ExtCurrency: "RUB"}, nil
}

func (g *Gateway) Status(ctx context.Context, p core.Payment) (core.GatewayEvent, error) {
	remote, err := g.fetch(ctx, p.ExternalID)
	if err != nil {
		return core.GatewayEvent{}, err
	}
	ev := core.GatewayEvent{ExternalID: p.ExternalID, Status: mapStatus(remote.Status)}
	if ev.Status == core.PayPaid {
		if remote.IsTest {
			return ev, errors.New("TabPay: платёж тестовый, зачислять нельзя")
		}
		ev.ExtAmount, err = remote.kopecks()
	}
	return ev, err
}

func (g *Gateway) fetch(ctx context.Context, id string) (apiPay, error) {
	var out apiPay
	err := g.do(ctx, http.MethodGet, "/v1/payments/"+url.PathEscape(id), nil, &out)
	return out, err
}

// Verify проверяет подпись вебхука, затем спрашивает статус и сумму у API: верим только ему.
// Недоступный API это ошибка: касса повторит вебхук позже.
func (g *Gateway) Verify(r *http.Request) (core.GatewayEvent, error) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		return core.GatewayEvent{}, errors.New("вебхук кассы TabPay не прочитан")
	}
	if !g.signedOK(body, r.Header.Get("X-Timestamp"), r.Header.Get("X-Signature-V2")) {
		return core.GatewayEvent{}, pay.ErrBadSignature
	}
	var hook apiPay
	if err := json.Unmarshal(body, &hook); err != nil {
		return core.GatewayEvent{}, errors.New("вебхук кассы TabPay не разобран")
	}
	ev := core.GatewayEvent{ExternalID: hook.ID, Status: mapStatus(hook.Status)}
	if n, ok := strings.CutPrefix(hook.OrderID, orderPrefix); ok {
		ev.PaymentID, _ = strconv.ParseInt(n, 10, 64)
	}
	if ev.ExternalID == "" {
		return ev, errors.New("в вебхуке кассы TabPay нет номера платежа")
	}
	if ev.Status != core.PayPaid && ev.Status != core.PayRefunded {
		return ev, nil
	}
	remote, err := g.fetch(r.Context(), ev.ExternalID)
	if err != nil {
		return ev, err
	}
	if mapStatus(remote.Status) != ev.Status {
		return ev, errors.New("касса TabPay не подтвердила статус из вебхука")
	}
	if ev.Status == core.PayRefunded {
		return ev, nil
	}
	if hook.Test || remote.IsTest {
		return ev, errors.New("TabPay: платёж тестовый, зачислять нельзя")
	}
	if ev.ExtAmount, err = remote.kopecks(); err != nil {
		return ev, err
	}
	if hookAmount, err := hook.kopecks(); err == nil && hookAmount != ev.ExtAmount {
		ev.ExtAmount = hookAmount // сумма вебхука не сошлась с API: ядро отклонит зачисление
	}
	return ev, nil
}

// signedOK сверяет X-Signature-V2: hex HMAC-SHA256 от "метка.тело" секретом подписи.
// Метка должна быть свежей, сравнение за постоянное время.
func (g *Gateway) signedOK(body []byte, stamp, sig string) bool {
	key := g.cfg.Secret(keySecret)
	stamp = strings.TrimSpace(stamp)
	sec, err := strconv.ParseInt(stamp, 10, 64)
	if key == "" || err != nil || sec < 0 {
		return false
	}
	if d := g.now().Sub(time.Unix(sec, 0)); d > maxSkew || d < -maxSkew {
		return false
	}
	got, err := hex.DecodeString(strings.TrimSpace(sig))
	return err == nil && hmac.Equal(got, sign(stamp, body, key))
}

// sign считает подпись вебхука.
func sign(stamp string, body []byte, key string) []byte {
	m := hmac.New(sha256.New, []byte(key))
	m.Write([]byte(stamp + "."))
	m.Write(body)
	return m.Sum(nil)
}

func mapStatus(s string) core.PayStatus {
	switch strings.ToUpper(s) {
	case "SUCCESS":
		return core.PayPaid
	case "FAILED", "EXPIRED", "CANCELED":
		return core.PayFailed
	case "REFUNDED":
		return core.PayRefunded // возврат после оплаты: ядро отменит зачисление
	}
	return core.PayPending // CREATED, PENDING
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
		return errors.New("не собран запрос к TabPay")
	}
	req.Header.Set("X-Api-Key", g.cfg.Secret(keyAPI))
	req.Header.Set("Content-Type", "application/json")
	resp, err := g.client.Do(req)
	if err != nil {
		return errors.New("касса TabPay недоступна")
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("касса TabPay ответила кодом %d", resp.StatusCode)
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(out); err != nil {
		return errors.New("ответ кассы TabPay не разобран")
	}
	return nil
}
