// Package mulenpay это касса MulenPay: оплата картами и СБП по ссылке.
package mulenpay

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/bini69-oi/worker/core"
	"github.com/bini69-oi/worker/pay"
	"github.com/bini69-oi/worker/settings"
)

// DefaultBaseURL это адрес API MulenPay.
const DefaultBaseURL = "https://mulenpay.ru/api"

// uuidPrefix метит наш номер платежа в поле uuid, чтобы узнать его в вебхуке.
const uuidPrefix = "worker-"

const (
	keyAPI     = "mulenpay.api_key"
	keyShop    = "mulenpay.shop_id"
	keySecret  = "mulenpay.secret_key"
	keyReturn  = "mulenpay.return_url"
	keyVAT     = "mulenpay.vat_code"
	keySubject = "mulenpay.payment_subject"
	keyMode    = "mulenpay.payment_mode"
)

func init() {
	settings.AddGateway("mulenpay", "Карты и СБП",
		settings.Def{Key: keyAPI, Kind: settings.KindString, Secret: true, Group: "pay.mulenpay",
			Desc: "API-ключ из кабинета MulenPay"},
		settings.Def{Key: keyShop, Kind: settings.KindString, Secret: true, Group: "pay.mulenpay",
			Desc: "Номер магазина (shopId) из кабинета MulenPay"},
		settings.Def{Key: keySecret, Kind: settings.KindString, Secret: true, Group: "pay.mulenpay",
			Desc: "Секретный ключ магазина: им подписаны счета и вебхуки"},
		settings.Def{Key: keyReturn, Kind: settings.KindString, Group: "pay.mulenpay",
			Desc: "Адрес сайта для кнопки возврата, {payment} заменится номером платежа. Пусто: обратно в бота"},
		settings.Def{Key: keyVAT, Kind: settings.KindInt, Default: "0", Group: "pay.mulenpay",
			Desc: "Код НДС в чеке (0 без НДС)"},
		settings.Def{Key: keySubject, Kind: settings.KindInt, Default: "4", Group: "pay.mulenpay",
			Desc: "Признак предмета расчёта в чеке (4 услуга)"},
		settings.Def{Key: keyMode, Kind: settings.KindInt, Default: "4", Group: "pay.mulenpay",
			Desc: "Признак способа расчёта в чеке (4 полный расчёт)"},
	)
}

// Config это настройки, которые касса читает на лету.
type Config interface {
	String(key string) string
	Int(key string) int64
	Secret(key string) string
}

// Gateway это касса MulenPay.
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

func (g *Gateway) Name() string { return "mulenpay" }

func (g *Gateway) Configured() bool {
	return g.cfg.Secret(keyAPI) != "" && g.cfg.Secret(keyShop) != "" && g.cfg.Secret(keySecret) != ""
}

// flex это поле, которое касса отдаёт то строкой, то числом.
type flex string

func (f *flex) UnmarshalJSON(b []byte) error {
	var s string
	if json.Unmarshal(b, &s) == nil {
		*f = flex(s)
		return nil
	}
	if string(b) == "null" {
		return nil
	}
	*f = flex(b)
	return nil
}

// apiPay это платёж в ответах API.
type apiPay struct {
	ID         flex   `json:"id"`
	UUID       string `json:"uuid"`
	Status     flex   `json:"status"`
	Amount     flex   `json:"amount"`
	Currency   string `json:"currency"`
	PaymentURL string `json:"paymentUrl"`
}

// reply это ответ API: платёж лежит в payment или прямо в корне.
type reply struct {
	apiPay
	Success *bool   `json:"success"`
	Payment *apiPay `json:"payment"`
}

func (r reply) pay() (apiPay, bool) {
	if r.Success != nil && !*r.Success {
		return apiPay{}, false
	}
	if r.Payment != nil {
		return *r.Payment, r.Payment.ID != ""
	}
	return r.apiPay, r.ID != ""
}

var digits = regexp.MustCompile(`^\d+$`)

func (g *Gateway) Create(ctx context.Context, p core.Payment) (core.Invoice, error) {
	amount, shop := rubles(p.Amount), strings.TrimSpace(g.cfg.Secret(keyShop))
	var shopID any = shop
	if digits.MatchString(shop) {
		shopID = json.Number(shop)
	}
	desc := fmt.Sprintf("Пополнение %d", p.ID)
	req := map[string]any{
		"currency":    "rub",
		"amount":      amount,
		"uuid":        uuidPrefix + strconv.FormatInt(p.ID, 10),
		"shopId":      shopID,
		"description": desc,
		"language":    "ru",
		"sign":        createSign("rub", amount, shop, g.cfg.Secret(keySecret)),
		"items": []map[string]any{{
			"description": desc, "quantity": 1, "price": json.Number(amount),
			"vat_code":        g.cfg.Int(keyVAT),
			"payment_subject": g.cfg.Int(keySubject),
			"payment_mode":    g.cfg.Int(keyMode),
		}},
	}
	if back := pay.ReturnURL(g.cfg.String(keyReturn), g.Return, p.ID); back != "" {
		req["website_url"] = back
	}
	var res reply
	if err := g.do(ctx, http.MethodPost, "/v2/payments", req, &res); err != nil {
		return core.Invoice{}, err
	}
	got, ok := res.pay()
	if !ok || got.PaymentURL == "" {
		return core.Invoice{}, errors.New("касса MulenPay не вернула номер платежа или ссылку")
	}
	return core.Invoice{ExternalID: string(got.ID), URL: got.PaymentURL, ExtAmount: p.Amount, ExtCurrency: "RUB"}, nil
}

func (g *Gateway) Status(ctx context.Context, p core.Payment) (core.GatewayEvent, error) {
	return g.confirmed(ctx, p.ExternalID)
}

// confirmed спрашивает платёж у API и собирает событие только из его ответа: статус и сумма отсюда.
func (g *Gateway) confirmed(ctx context.Context, id string) (core.GatewayEvent, error) {
	var res reply
	if err := g.do(ctx, http.MethodGet, "/v2/payments/"+url.PathEscape(id), nil, &res); err != nil {
		return core.GatewayEvent{}, err
	}
	got, ok := res.pay()
	if !ok {
		return core.GatewayEvent{}, errors.New("касса MulenPay не знает такого платежа")
	}
	ev := core.GatewayEvent{ExternalID: id, Status: mapStatus(string(got.Status))}
	if n, found := strings.CutPrefix(got.UUID, uuidPrefix); found {
		ev.PaymentID, _ = strconv.ParseInt(n, 10, 64)
	}
	if ev.Status != core.PayPaid {
		return ev, nil
	}
	// Без суммы или не в рублях зачислять нечего: подставлять свою значило бы поверить на слово.
	if got.Currency != "" && !strings.EqualFold(got.Currency, "rub") {
		return ev, fmt.Errorf("платёж MulenPay не в рублях: %s", got.Currency)
	}
	if got.Amount == "" {
		return ev, errors.New("в ответе MulenPay нет суммы")
	}
	var err error
	ev.ExtAmount, err = kopecks(string(got.Amount))
	return ev, err
}

// Verify проверяет подпись вебхука, если она есть, и в любом случае переспрашивает платёж у API:
// у MulenPay вебхук служит сигналом, верим только ответу API.
func (g *Gateway) Verify(r *http.Request) (core.GatewayEvent, error) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		return core.GatewayEvent{}, errors.New("вебхук кассы MulenPay не прочитан")
	}
	raw, py, sign, err := hookFields(body)
	if err != nil {
		return core.GatewayEvent{}, errors.New("вебхук кассы MulenPay не разобран")
	}
	if sign != "" && !signOK(raw, py, sign, g.cfg.Secret(keySecret)) {
		return core.GatewayEvent{}, pay.ErrBadSignature
	}
	var hook apiPay
	if err := json.Unmarshal(body, &hook); err != nil || hook.ID == "" {
		return core.GatewayEvent{}, errors.New("в вебхуке кассы MulenPay нет номера платежа")
	}
	ev, err := g.confirmed(r.Context(), string(hook.ID))
	if err != nil || ev.Status != core.PayPaid || hook.Amount == "" {
		return ev, err
	}
	if k, perr := kopecks(string(hook.Amount)); perr != nil || k != ev.ExtAmount {
		ev.ExtAmount = k // вебхук не сошёлся с API: подставляем его сумму, ядро отклонит зачисление
	}
	return ev, nil
}

// mapStatus переводит статус кассы: число из API или слово из вебхука.
func mapStatus(s string) core.PayStatus {
	switch strings.ToLower(s) {
	case "3", "success", "paid", "succeeded":
		return core.PayPaid
	case "2", "4", "cancel", "canceled", "cancelled", "error", "fail", "failed", "expired":
		return core.PayFailed
	}
	return core.PayPending // создан, в обработке, холд
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
		return errors.New("не собран запрос к MulenPay")
	}
	req.Header.Set("Authorization", "Bearer "+g.cfg.Secret(keyAPI))
	req.Header.Set("Content-Type", "application/json")
	resp, err := g.client.Do(req)
	if err != nil {
		return errors.New("касса MulenPay недоступна")
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("касса MulenPay ответила кодом %d", resp.StatusCode)
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(out); err != nil {
		return errors.New("ответ кассы MulenPay не разобран")
	}
	return nil
}
