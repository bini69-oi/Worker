// Package stripe это касса Stripe (зарубежные карты): Checkout Session в режиме payment.
//
// Stripe может не принимать рубли, поэтому касса работает в своей валюте (stripe.currency) и сама
// пересчитывает копейки по курсу из настроек (stripe.rate). Вебхук подписан секретом endpoint (whsec_...),
// подпись в заголовке Stripe-Signature. Только стандартная библиотека, без stripe-go.
package stripe

import (
	"context"
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

// DefaultBaseURL это адрес API Stripe.
const DefaultBaseURL = "https://api.stripe.com"

const (
	keySecret   = "stripe.secret_key"
	keyWebhook  = "stripe.webhook_secret"
	keyCurrency = "stripe.currency"
	keyRate     = "stripe.rate"
	keyReturn   = "stripe.return_url"

	maxBody = 1 << 20
)

func init() {
	settings.AddGateway("stripe", "Карты зарубежных банков (Stripe)",
		settings.Def{Key: keySecret, Kind: settings.KindString, Secret: true, Group: "pay.stripe",
			Desc: "Секретный ключ Stripe (sk_live_... или sk_test_...) из Developers, API keys"},
		settings.Def{Key: keyWebhook, Kind: settings.KindString, Secret: true, Group: "pay.stripe",
			Desc: "Секрет вебхука (whsec_...) из Developers, Webhooks, ваш endpoint https://ваш-сайт/pay/stripe"},
		settings.Def{Key: keyCurrency, Kind: settings.KindString, Default: "usd", Group: "pay.stripe",
			Desc: "Валюта счёта: три буквы, например usd или eur. Должна быть двузначной (центы), как доллар и евро"},
		settings.Def{Key: keyRate, Kind: settings.KindInt, Default: "0", Group: "pay.stripe",
			Desc: "Курс: сколько копеек стоит одна основная единица валюты, например 9500 значит 95 рублей за 1 доллар. " +
				"Только целое число, сумма в центах округляется вверх. 0: касса не видна, пока курс не задан"},
		settings.Def{Key: keyReturn, Kind: settings.KindString, Group: "pay.stripe",
			Desc: "Куда вернуть абонента после оплаты, {payment} заменится номером платежа. Stripe требует адрес: пусто значит обратно в бота"},
	)
}

// Config это настройки, которые касса читает на лету.
type Config interface {
	String(key string) string
	Int(key string) int64
	Secret(key string) string
}

// Gateway это касса Stripe.
type Gateway struct {
	cfg     Config
	baseURL string
	client  *http.Client
	now     func() time.Time
	// Return это адрес возврата по умолчанию, если настройка пустая. Ставит сборка: диплинк в бота.
	// Адрес вебхука сборка не ставит: он вписывается в кабинете Stripe.
	Return func(paymentID int64) string
}

var _ pay.Gateway = (*Gateway)(nil)

// New собирает кассу. Пустой baseURL означает боевой адрес, nil вместо клиента означает клиент с таймаутом.
func New(cfg Config, baseURL string, client *http.Client) *Gateway {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	return &Gateway{cfg: cfg, baseURL: strings.TrimRight(baseURL, "/"), client: client, now: time.Now}
}

func (g *Gateway) Name() string { return "stripe" }

func (g *Gateway) Configured() bool {
	_, ok := currencyCode(g.cfg.String(keyCurrency))
	return g.cfg.Secret(keySecret) != "" && g.cfg.Secret(keyWebhook) != "" && g.cfg.Int(keyRate) > 0 && ok
}

// session это Checkout Session в ответе API и в вебхуке (нужные нам поля).
type session struct {
	ID                string            `json:"id"`
	URL               string            `json:"url"`
	Status            string            `json:"status"`
	PaymentStatus     string            `json:"payment_status"`
	AmountTotal       int64             `json:"amount_total"`
	Currency          string            `json:"currency"`
	ClientReferenceID string            `json:"client_reference_id"`
	Metadata          map[string]string `json:"metadata"`
}

// paymentID достаёт наш номер из metadata, запасной путь client_reference_id.
func paymentID(meta map[string]string, ref string) int64 {
	for _, s := range []string{meta["payment_id"], ref} {
		if n, err := strconv.ParseInt(s, 10, 64); err == nil && n > 0 {
			return n
		}
	}
	return 0
}

// Create выставляет Checkout Session в валюте кассы. Сумма в центах считается по курсу из настроек.
func (g *Gateway) Create(ctx context.Context, p core.Payment) (core.Invoice, error) {
	cur, ok := currencyCode(g.cfg.String(keyCurrency))
	if !ok {
		return core.Invoice{}, errors.New("в настройке валюты Stripe нужны три буквы")
	}
	minor, err := minorFor(p.Amount, g.cfg.Int(keyRate))
	if err != nil {
		return core.Invoice{}, err
	}
	back := pay.ReturnURL(g.cfg.String(keyReturn), g.Return, p.ID)
	if back == "" {
		return core.Invoice{}, errors.New("для Stripe нужен адрес возврата (stripe.return_url)")
	}
	id := strconv.FormatInt(p.ID, 10)
	f := url.Values{
		"mode":                 {"payment"},
		"success_url":          {back},
		"cancel_url":           {back},
		"client_reference_id":  {id},
		"metadata[payment_id]": {id},
		"payment_intent_data[metadata][payment_id]":     {id},
		"line_items[0][quantity]":                       {"1"},
		"line_items[0][price_data][currency]":           {cur},
		"line_items[0][price_data][unit_amount]":        {strconv.FormatInt(minor, 10)},
		"line_items[0][price_data][product_data][name]": {"Balance top-up " + id},
	}
	var res session
	if err := g.call(ctx, http.MethodPost, "/v1/checkout/sessions", f, &res); err != nil {
		return core.Invoice{}, err
	}
	if res.ID == "" || res.URL == "" {
		return core.Invoice{}, errors.New("касса Stripe не вернула сессию или ссылку")
	}
	return core.Invoice{ExternalID: res.ID, URL: res.URL, ExtAmount: minor, ExtCurrency: strings.ToUpper(cur)}, nil
}

// Status спрашивает сессию у Stripe. Оплата верится только ответу API.
func (g *Gateway) Status(ctx context.Context, p core.Payment) (core.GatewayEvent, error) {
	if !strings.HasPrefix(p.ExternalID, "cs_") {
		return core.GatewayEvent{}, errors.New("у платежа нет номера сессии Stripe")
	}
	var res session
	if err := g.call(ctx, http.MethodGet, "/v1/checkout/sessions/"+url.PathEscape(p.ExternalID), nil, &res); err != nil {
		return core.GatewayEvent{}, err
	}
	if res.ID != p.ExternalID {
		return core.GatewayEvent{}, errors.New("касса Stripe ответила про другую сессию")
	}
	ev := core.GatewayEvent{PaymentID: p.ID, ExternalID: res.ID, Status: core.PayPending}
	switch {
	case res.PaymentStatus == "paid":
		ev.Status = core.PayPaid
	case res.Status == "expired":
		ev.Status = core.PayFailed
	}
	return g.withAmount(ev, res)
}

// withAmount кладёт в оплаченное событие сумму сессии, если валюта та, что в настройках.
// Другая валюта это ошибка: сверять центы с чужой валютой нельзя.
func (g *Gateway) withAmount(ev core.GatewayEvent, s session) (core.GatewayEvent, error) {
	if ev.Status != core.PayPaid {
		return ev, nil
	}
	cur, _ := currencyCode(g.cfg.String(keyCurrency))
	if !strings.EqualFold(s.Currency, cur) {
		return core.GatewayEvent{}, errors.New("валюта оплаты Stripe не совпала с настройкой")
	}
	ev.ExtAmount = s.AmountTotal
	return ev, nil
}

// call шлёт запрос к API. Ключ идёт только в заголовок и в ошибки не попадает.
func (g *Gateway) call(ctx context.Context, method, path string, form url.Values, out *session) error {
	key := g.cfg.Secret(keySecret)
	if key == "" {
		return errors.New("касса Stripe не настроена")
	}
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req, err := http.NewRequestWithContext(ctx, method, g.baseURL+path, body)
	if err != nil {
		return errors.New("запрос Stripe не собран")
	}
	req.Header.Set("Authorization", "Bearer "+key)
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	resp, err := g.client.Do(req)
	if err != nil {
		// Ошибку транспорта не отдаём как есть: в ней может быть адрес запроса.
		return errors.New("касса Stripe не отвечает")
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return fmt.Errorf("ответ Stripe не прочитан (код %d)", resp.StatusCode)
	}
	if resp.StatusCode >= 300 {
		var e struct {
			Error struct {
				Code string `json:"code"`
			} `json:"error"`
		}
		_ = json.Unmarshal(raw, &e)
		return fmt.Errorf("касса Stripe отказала (код %d, %s)", resp.StatusCode, e.Error.Code)
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return errors.New("ответ Stripe не разобран")
	}
	return nil
}
