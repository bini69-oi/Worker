// Package overpay это касса Overpay: оплата по ссылке, API с Basic-авторизацией и по желанию mTLS.
// Вебхук Overpay не подписан, поэтому ему не верят: Verify всегда переспрашивает статус и сумму у API.
package overpay

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

	"github.com/bini69-oi/worker/core"
	"github.com/bini69-oi/worker/pay"
	"github.com/bini69-oi/worker/settings"
)

// DefaultBaseURL это адрес API Overpay.
const DefaultBaseURL = "https://api.overpay.io"

// orderPrefix метит наш номер платежа в merchantTransactionId, чтобы узнать его в вебхуке.
const orderPrefix = "worker-"

const (
	keyUser    = "overpay.username"
	keyPass    = "overpay.password"
	keyProject = "overpay.project_id"
	keyCert    = "overpay.client_cert_pem"
	keyKey     = "overpay.client_key_pem"
	keyMethods = "overpay.methods"
	keyLife    = "overpay.lifetime_minutes"
	keyReturn  = "overpay.return_url"
)

func init() {
	settings.AddGateway("overpay", "Overpay",
		settings.Def{Key: keyUser, Kind: settings.KindString, Secret: true, Group: "pay.overpay",
			Desc: "Логин API из кабинета Overpay (Basic-авторизация)"},
		settings.Def{Key: keyPass, Kind: settings.KindString, Secret: true, Group: "pay.overpay",
			Desc: "Пароль API из кабинета Overpay"},
		settings.Def{Key: keyProject, Kind: settings.KindString, Group: "pay.overpay",
			Desc: "Номер проекта (projectId) из кабинета Overpay"},
		settings.Def{Key: keyCert, Kind: settings.KindString, Secret: true, Group: "pay.overpay",
			Desc: "Клиентский сертификат для mTLS в формате PEM. Пусто: без mTLS. P12 переведите в PEM через openssl"},
		settings.Def{Key: keyKey, Kind: settings.KindString, Secret: true, Group: "pay.overpay",
			Desc: "Закрытый ключ к сертификату в формате PEM (без пароля)"},
		settings.Def{Key: keyMethods, Kind: settings.KindString, Default: "card,fps", Group: "pay.overpay",
			Desc: "Способы оплаты через запятую: card, fps (СБП). Пусто: выбор на странице кассы"},
		settings.Def{Key: keyLife, Kind: settings.KindInt, Default: "1440", Group: "pay.overpay",
			Desc: "Сколько минут живёт счёт"},
		settings.Def{Key: keyReturn, Kind: settings.KindString, Group: "pay.overpay",
			Desc: "Куда вернуть абонента после оплаты, {payment} заменится номером платежа. Пусто: обратно в бота"},
	)
}

// Config это настройки, которые касса читает на лету.
type Config interface {
	String(key string) string
	Int(key string) int64
	Secret(key string) string
}

// Gateway это касса Overpay.
type Gateway struct {
	cfg     Config
	baseURL string
	client  *http.Client
	// Return это адрес возврата по умолчанию, если настройка пустая. Ставит сборка: диплинк в бота.
	Return func(paymentID int64) string
}

var _ pay.Gateway = (*Gateway)(nil)

// New собирает кассу. Пустой baseURL означает боевой адрес. Пустой client означает клиент
// с таймаутом и сертификатом из настроек (собирается на каждый запрос).
func New(cfg Config, baseURL string, client *http.Client) *Gateway {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	return &Gateway{cfg: cfg, baseURL: strings.TrimRight(baseURL, "/"), client: client}
}

func (g *Gateway) Name() string { return "overpay" }

func (g *Gateway) Configured() bool {
	return g.cfg.Secret(keyUser) != "" && g.cfg.Secret(keyPass) != "" && g.cfg.String(keyProject) != ""
}

type createReq struct {
	Amount     string   `json:"amount"`
	Currency   string   `json:"currency"`
	Lifetime   int64    `json:"livetimeMinutes"`
	ProjectID  string   `json:"projectId"`
	MerchantID string   `json:"merchantTransactionId"`
	Desc       string   `json:"description"`
	ReturnURL  string   `json:"returnUrl,omitempty"`
	Methods    []string `json:"paymentMethods,omitempty"`
}

// order это заказ в ответе API и в вебхуке.
type order struct {
	ID         json.Number `json:"id"`
	MerchantID string      `json:"merchantTransactionId"`
	Status     string      `json:"status"`
	Amount     json.Number `json:"amount"`
	Currency   string      `json:"currency"`
	ResultURL  string      `json:"resultUrl"`
}

// orderResp это ответ GET /orders/{id}: заказ лежит либо в корне, либо в списке orders.
type orderResp struct {
	order
	Orders []order `json:"orders"`
}

func (o orderResp) pick() order {
	if len(o.Orders) > 0 {
		return o.Orders[0]
	}
	return o.order
}

// rubAmount отдаёт сумму заказа: она обязана быть, валюта, если указана, только рубли.
func (o order) rubAmount() (int64, error) {
	if o.Currency != "" && !strings.EqualFold(o.Currency, "RUB") {
		return 0, fmt.Errorf("платёж Overpay не в рублях: %s", o.Currency)
	}
	if o.Amount == "" {
		return 0, errors.New("в ответе Overpay нет суммы")
	}
	return pay.Kopecks(string(o.Amount))
}

func (g *Gateway) Create(ctx context.Context, p core.Payment) (core.Invoice, error) {
	req := createReq{
		Amount:     pay.Rubles(p.Amount),
		Currency:   "RUB",
		Lifetime:   g.cfg.Int(keyLife),
		ProjectID:  g.cfg.String(keyProject),
		MerchantID: orderPrefix + strconv.FormatInt(p.ID, 10),
		Desc:       fmt.Sprintf("Пополнение %d", p.ID),
		ReturnURL:  pay.ReturnURL(g.cfg.String(keyReturn), g.Return, p.ID),
	}
	for _, m := range strings.Split(g.cfg.String(keyMethods), ",") {
		if m = strings.TrimSpace(m); m != "" {
			req.Methods = append(req.Methods, m)
		}
	}
	var out order
	if err := g.do(ctx, http.MethodPost, "/orders/", req, &out); err != nil {
		return core.Invoice{}, err
	}
	if out.ID == "" || out.ResultURL == "" {
		return core.Invoice{}, errors.New("касса Overpay не вернула номер заказа или ссылку")
	}
	return core.Invoice{ExternalID: string(out.ID), URL: out.ResultURL, ExtAmount: p.Amount, ExtCurrency: "RUB"}, nil
}

// fetch спрашивает заказ у API. Любая ошибка (сеть, код, разбор) уходит наверх: деньги по ней не зачисляются.
func (g *Gateway) fetch(ctx context.Context, id string) (order, error) {
	var out orderResp
	if err := g.do(ctx, http.MethodGet, "/orders/"+url.PathEscape(id), nil, &out); err != nil {
		return order{}, err
	}
	o := out.pick()
	if o.Status == "" {
		return order{}, errors.New("в ответе Overpay нет статуса")
	}
	return o, nil
}

// event собирает событие из заказа, который вернул API.
func event(id string, o order) (core.GatewayEvent, error) {
	ev := core.GatewayEvent{ExternalID: id, Status: mapStatus(o.Status)}
	if n, ok := strings.CutPrefix(o.MerchantID, orderPrefix); ok {
		ev.PaymentID, _ = strconv.ParseInt(n, 10, 64)
	}
	if ev.Status == core.PayPaid {
		var err error
		ev.ExtAmount, err = o.rubAmount()
		return ev, err
	}
	return ev, nil
}

func (g *Gateway) Status(ctx context.Context, p core.Payment) (core.GatewayEvent, error) {
	o, err := g.fetch(ctx, p.ExternalID)
	if err != nil {
		return core.GatewayEvent{}, err
	}
	ev, err := event(p.ExternalID, o)
	ev.PaymentID = 0 // платёж известен по p, номер из заказа не нужен
	return ev, err
}

// Verify разбирает вебхук как сигнал «заказ изменился». Подписи у вебхука Overpay нет,
// поэтому из него берётся только номер заказа, а статус и сумму отдаёт API.
// API недоступен или не знает заказ: ошибка, деньги не зачисляются, касса повторит вебхук.
func (g *Gateway) Verify(r *http.Request) (core.GatewayEvent, error) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		return core.GatewayEvent{}, errors.New("вебхук кассы Overpay не прочитан")
	}
	var hook order
	if err := json.Unmarshal(body, &hook); err != nil || hook.ID == "" {
		return core.GatewayEvent{}, errors.New("вебхук кассы Overpay не разобран или без номера заказа")
	}
	if !g.Configured() {
		return core.GatewayEvent{}, pay.ErrBadSignature
	}
	o, err := g.fetch(r.Context(), string(hook.ID))
	if err != nil {
		return core.GatewayEvent{}, err
	}
	if hook.MerchantID != "" && o.MerchantID != hook.MerchantID {
		return core.GatewayEvent{}, pay.ErrBadSignature // вебхук про один заказ, API отвечает про другой
	}
	return event(string(hook.ID), o)
}

func mapStatus(s string) core.PayStatus {
	switch strings.ToLower(s) {
	case "charged":
		return core.PayPaid
	case "declined", "rejected", "error":
		return core.PayFailed
	case "refunded", "chargeback", "representment", "reversed":
		return core.PayRefunded // возврат после оплаты: ядро отменит зачисление, для неоплаченного ничего не делает
	}
	return core.PayPending
}
