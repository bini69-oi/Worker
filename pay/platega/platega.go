// Package platega это касса Platega: оплата СБП и картами по ссылке.
package platega

import (
	"bytes"
	"context"
	"crypto/subtle"
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

// DefaultBaseURL это адрес API Platega.
const DefaultBaseURL = "https://app.platega.io"

// methodSBP это код способа оплаты «СБП».
const methodSBP = 2

// payloadPrefix метит наш номер платежа в поле payload, чтобы узнать его в вебхуке.
const payloadPrefix = "worker:"

func init() {
	settings.AddGateway("platega", "СБП и карты",
		settings.Def{Key: "platega.merchant_id", Kind: settings.KindString, Group: "pay.platega",
			Desc: "Идентификатор мерчанта из кабинета Platega"},
		settings.Def{Key: "platega.secret", Kind: settings.KindString, Secret: true, Group: "pay.platega",
			Desc: "Секрет мерчанта из кабинета Platega"},
		settings.Def{Key: "platega.method", Kind: settings.KindInt, Default: strconv.Itoa(methodSBP), Group: "pay.platega",
			Desc: "Код способа оплаты в Platega (2 это СБП)"},
		settings.Def{Key: "platega.return_url", Kind: settings.KindString, Group: "pay.platega",
			Desc: "Куда вернуть абонента после оплаты, можно пусто"},
	)
}

// Config это настройки, которые касса читает на лету.
type Config interface {
	String(key string) string
	Int(key string) int64
	Secret(key string) string
}

// Gateway это касса Platega.
type Gateway struct {
	cfg     Config
	baseURL string
	client  *http.Client
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

func (g *Gateway) Name() string { return "platega" }

func (g *Gateway) Configured() bool {
	return g.cfg.String("platega.merchant_id") != "" && g.cfg.Secret("platega.secret") != ""
}

type createReq struct {
	PaymentMethod  int64   `json:"paymentMethod"`
	PaymentDetails details `json:"paymentDetails"`
	Description    string  `json:"description"`
	Return         string  `json:"return,omitempty"`
	FailedURL      string  `json:"failedUrl,omitempty"`
	Payload        string  `json:"payload"`
}

type details struct {
	Amount   json.Number `json:"amount"`
	Currency string      `json:"currency"`
}

// apiTx это общие поля ответа и вебхука Platega.
type apiTx struct {
	ID            string      `json:"id"`
	TransactionID string      `json:"transactionId"`
	Redirect      string      `json:"redirect"`
	URL           string      `json:"url"`
	Status        string      `json:"status"`
	Amount        json.Number `json:"amount"`
	Currency      string      `json:"currency"`
	Payload       string      `json:"payload"`
	Details       *details    `json:"paymentDetails"`
}

func (t apiTx) id() string {
	if t.TransactionID != "" {
		return t.TransactionID
	}
	return t.ID
}

// amount отдаёт сумму в копейках из верхнего поля или из paymentDetails. Второе значение: сумма есть.
func (t apiTx) amount() (int64, bool, error) {
	n := t.Amount
	if n == "" && t.Details != nil {
		n = t.Details.Amount
	}
	if n == "" {
		return 0, false, nil
	}
	k, err := kopecks(string(n))
	return k, err == nil, err
}

// rubAmount отдаёт сумму из ответа API для зачисления: сумма обязана быть, валюта, если указана, только рубли.
// Без суммы зачислять нечего: подставлять свою значило бы поверить кассе на слово.
func (t apiTx) rubAmount() (int64, error) {
	cur := t.Currency
	if cur == "" && t.Details != nil {
		cur = t.Details.Currency
	}
	if cur != "" && !strings.EqualFold(cur, "RUB") {
		return 0, fmt.Errorf("платёж Platega не в рублях: %s", cur)
	}
	k, ok, err := t.amount()
	if err != nil {
		return 0, err
	}
	if !ok {
		return 0, errors.New("в ответе Platega нет суммы")
	}
	return k, nil
}

func (g *Gateway) Create(ctx context.Context, p core.Payment) (core.Invoice, error) {
	req := createReq{
		PaymentMethod:  g.cfg.Int("platega.method"),
		PaymentDetails: details{Amount: json.Number(rubles(p.Amount)), Currency: "RUB"},
		Description:    fmt.Sprintf("Пополнение %d", p.ID),
		Return:         g.cfg.String("platega.return_url"),
		Payload:        payloadPrefix + strconv.FormatInt(p.ID, 10),
	}
	if req.PaymentMethod == 0 {
		req.PaymentMethod = methodSBP
	}
	req.FailedURL = req.Return
	var tx apiTx
	if err := g.do(ctx, http.MethodPost, "/transaction/process", req, &tx); err != nil {
		return core.Invoice{}, err
	}
	link := tx.Redirect
	if link == "" {
		link = tx.URL
	}
	if tx.id() == "" || link == "" {
		return core.Invoice{}, errors.New("касса Platega не вернула номер платежа или ссылку")
	}
	return core.Invoice{ExternalID: tx.id(), URL: link, ExtAmount: p.Amount, ExtCurrency: "RUB"}, nil
}

func (g *Gateway) Status(ctx context.Context, p core.Payment) (core.GatewayEvent, error) {
	tx, err := g.fetch(ctx, p.ExternalID)
	if err != nil {
		return core.GatewayEvent{}, err
	}
	ev := core.GatewayEvent{ExternalID: p.ExternalID, Status: mapStatus(tx.Status)}
	if ev.Status != core.PayPaid {
		return ev, nil
	}
	ev.ExtAmount, err = tx.rubAmount()
	return ev, err
}

func (g *Gateway) fetch(ctx context.Context, id string) (apiTx, error) {
	var tx apiTx
	err := g.do(ctx, http.MethodGet, "/transaction/"+url.PathEscape(id), nil, &tx)
	return tx, err
}

// Verify сверяет заголовки X-MerchantId и X-Secret с настройками за постоянное время,
// затем переспрашивает статус у API: вебхуку на слово не верим.
func (g *Gateway) Verify(r *http.Request) (core.GatewayEvent, error) {
	merchant, secret := g.cfg.String("platega.merchant_id"), g.cfg.Secret("platega.secret")
	okM := subtle.ConstantTimeCompare([]byte(r.Header.Get("X-MerchantId")), []byte(merchant))
	okS := subtle.ConstantTimeCompare([]byte(r.Header.Get("X-Secret")), []byte(secret))
	if merchant == "" || secret == "" || okM&okS != 1 {
		return core.GatewayEvent{}, pay.ErrBadSignature
	}
	var hook apiTx
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&hook); err != nil {
		return core.GatewayEvent{}, errors.New("вебхук кассы Platega не разобран")
	}
	ev := core.GatewayEvent{ExternalID: hook.id(), Status: mapStatus(hook.Status)}
	if n, ok := strings.CutPrefix(hook.Payload, payloadPrefix); ok {
		ev.PaymentID, _ = strconv.ParseInt(n, 10, 64)
	}
	if ev.ExternalID == "" {
		return ev, errors.New("в вебхуке кассы Platega нет номера платежа")
	}
	hookAmount, hasHook, err := hook.amount()
	if err != nil {
		return ev, err
	}
	if ev.Status != core.PayPaid && ev.Status != core.PayRefunded {
		ev.ExtAmount = hookAmount
		return ev, nil
	}
	// Оплату и возврат подтверждает сам API: статус и сумма берутся оттуда.
	remote, err := g.fetch(r.Context(), ev.ExternalID)
	if err != nil {
		return ev, err
	}
	if mapStatus(remote.Status) != ev.Status {
		return ev, errors.New("касса Platega не подтвердила статус из вебхука")
	}
	if ev.Status == core.PayRefunded {
		return ev, nil
	}
	if ev.ExtAmount, err = remote.rubAmount(); err != nil {
		return ev, err
	}
	if hasHook && hookAmount != ev.ExtAmount {
		ev.ExtAmount = hookAmount // сумма вебхука не сошлась с API: ядро отклонит зачисление
	}
	return ev, nil
}

func mapStatus(s string) core.PayStatus {
	switch strings.ToUpper(s) {
	case "CONFIRMED":
		return core.PayPaid
	case "CANCELED", "FAILED", "EXPIRED":
		return core.PayFailed
	case "CHARGEBACKED", "CHARGEBACK", "REFUNDED":
		return core.PayRefunded // оспаривание или возврат после оплаты: ядро отменит зачисление
	}
	return core.PayPending
}

// do шлёт запрос в API. Секрет идёт только в заголовок и в ошибки не попадает.
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
		return errors.New("не собран запрос к Platega")
	}
	req.Header.Set("X-MerchantId", g.cfg.String("platega.merchant_id"))
	req.Header.Set("X-Secret", g.cfg.Secret("platega.secret"))
	req.Header.Set("Content-Type", "application/json")
	resp, err := g.client.Do(req)
	if err != nil {
		return errors.New("касса Platega недоступна")
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("касса Platega ответила кодом %d", resp.StatusCode)
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(out); err != nil {
		return errors.New("ответ кассы Platega не разобран")
	}
	return nil
}
