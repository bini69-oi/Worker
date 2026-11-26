// Package paypalych это касса PayPalych (pal24.pro, она же Pally): оплата картой и СБП по ссылке.
package paypalych

import (
	"bytes"
	"context"
	"crypto/md5" // подпись postback у кассы только md5, выбирать не из чего
	"crypto/subtle"
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

// DefaultBaseURL это адрес API PayPalych.
const DefaultBaseURL = "https://pal24.pro/api/v1"

// orderPrefix метит наш номер платежа в order_id, чтобы узнать его в postback.
const orderPrefix = "worker:"

const (
	keyToken  = "paypalych.token"
	keyShop   = "paypalych.shop_id"
	keyReturn = "paypalych.return_url"
)

func init() {
	settings.AddGateway("paypalych", "Карты и СБП",
		settings.Def{Key: keyToken, Kind: settings.KindString, Secret: true, Group: "pay.paypalych",
			Desc: "API-токен из кабинета PayPalych (он же подписывает postback)"},
		settings.Def{Key: keyShop, Kind: settings.KindString, Secret: true, Group: "pay.paypalych",
			Desc: "Идентификатор магазина (shop_id) из кабинета PayPalych"},
		settings.Def{Key: keyReturn, Kind: settings.KindString, Group: "pay.paypalych",
			Desc: "Куда вернуть абонента после оплаты, {payment} заменится номером платежа. Пусто: обратно в бота"},
	)
}

// Config это настройки, которые касса читает на лету.
type Config interface {
	String(key string) string
	Int(key string) int64
	Secret(key string) string
}

// Gateway это касса PayPalych. Адрес postback задаётся в кабинете магазина (Result URL), не в запросе.
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

func (g *Gateway) Name() string { return "paypalych" }

func (g *Gateway) Configured() bool {
	return strings.TrimSpace(g.cfg.Secret(keyToken)) != "" && strings.TrimSpace(g.cfg.Secret(keyShop)) != ""
}

// bill это ответ API про счёт (создание и статус).
type bill struct {
	Success     json.RawMessage `json:"success"`
	Message     string          `json:"message"`
	BillID      string          `json:"bill_id"`
	ID          string          `json:"id"`
	Status      string          `json:"status"`
	Amount      json.Number     `json:"amount"`
	CurrencyIn  string          `json:"currency_in"`
	LinkURL     string          `json:"link_url"`
	LinkPageURL string          `json:"link_page_url"`
}

// failed говорит, что касса явно ответила success=false (бывает и строкой "false").
func (b bill) failed() bool {
	s := strings.Trim(strings.TrimSpace(string(b.Success)), `"`)
	return strings.EqualFold(s, "false") || s == "0"
}

func (g *Gateway) Create(ctx context.Context, p core.Payment) (core.Invoice, error) {
	req := map[string]string{
		"amount":      pay.Rubles(p.Amount),
		"shop_id":     strings.TrimSpace(g.cfg.Secret(keyShop)),
		"order_id":    orderPrefix + strconv.FormatInt(p.ID, 10),
		"currency_in": "RUB",
		"type":        "normal",
		"description": fmt.Sprintf("Пополнение %d", p.ID),
	}
	if back := pay.ReturnURL(g.cfg.String(keyReturn), g.Return, p.ID); back != "" {
		req["success_url"], req["fail_url"] = back, back
	}
	var res bill
	if err := g.do(ctx, http.MethodPost, "/bill/create", nil, req, &res); err != nil {
		return core.Invoice{}, err
	}
	link := res.LinkPageURL
	if link == "" {
		link = res.LinkURL
	}
	if res.failed() || res.BillID == "" || link == "" {
		return core.Invoice{}, errors.New("касса PayPalych не вернула счёт или ссылку")
	}
	return core.Invoice{ExternalID: res.BillID, URL: link, ExtAmount: p.Amount, ExtCurrency: "RUB"}, nil
}

// Status спрашивает счёт у кассы по его номеру. Сумму берём из счёта: оплачен он на всю заявленную сумму.
func (g *Gateway) Status(ctx context.Context, p core.Payment) (core.GatewayEvent, error) {
	var res bill
	if err := g.do(ctx, http.MethodGet, "/bill/status", url.Values{"id": {p.ExternalID}}, nil, &res); err != nil {
		return core.GatewayEvent{}, err
	}
	if res.failed() {
		return core.GatewayEvent{}, errors.New("касса PayPalych не отдала статус счёта")
	}
	if res.ID != "" && res.ID != p.ExternalID {
		return core.GatewayEvent{}, errors.New("касса PayPalych ответила про другой счёт")
	}
	ev := core.GatewayEvent{ExternalID: p.ExternalID, Status: mapStatus(res.Status)}
	if ev.Status != core.PayPaid {
		return ev, nil
	}
	if res.CurrencyIn != "" && !strings.EqualFold(res.CurrencyIn, "RUB") {
		return ev, fmt.Errorf("счёт PayPalych не в рублях: %s", res.CurrencyIn)
	}
	if res.Amount == "" {
		return ev, errors.New("в ответе PayPalych нет суммы")
	}
	var err error
	ev.ExtAmount, err = pay.Kopecks(string(res.Amount))
	return ev, err
}

// sign это подпись postback: MD5("OutSum:InvId:токен") заглавным hex.
func sign(outSum, invID, token string) string {
	sum := md5.Sum([]byte(outSum + ":" + invID + ":" + token))
	return strings.ToUpper(hex.EncodeToString(sum[:]))
}

// Verify разбирает postback (форма, в кабинете можно включить JSON) и сверяет подпись за постоянное время.
// InvId в postback это наш order_id; на случай, если касса пришлёт номер счёта, понимаем и его.
func (g *Gateway) Verify(r *http.Request) (core.GatewayEvent, error) {
	f, err := postback(r)
	if err != nil {
		return core.GatewayEvent{}, err
	}
	token := strings.TrimSpace(g.cfg.Secret(keyToken))
	want := sign(f["OutSum"], f["InvId"], token)
	got := strings.ToUpper(strings.TrimSpace(f["SignatureValue"]))
	if token == "" || got == "" || subtle.ConstantTimeCompare([]byte(got), []byte(want)) != 1 {
		return core.GatewayEvent{}, pay.ErrBadSignature
	}
	ev := core.GatewayEvent{ExternalID: f["BillId"], Status: mapStatus(f["Status"])}
	if n, ok := strings.CutPrefix(f["InvId"], orderPrefix); ok {
		ev.PaymentID, _ = strconv.ParseInt(n, 10, 64)
	} else if ev.ExternalID == "" {
		ev.ExternalID = f["InvId"]
	}
	if ev.ExternalID == "" && ev.PaymentID == 0 {
		return ev, errors.New("в postback кассы PayPalych нет номера платежа")
	}
	if ev.ExtAmount, err = pay.Kopecks(f["OutSum"]); err != nil {
		return ev, err
	}
	// Подпись закрывает только сумму и номер заказа, статус в неё не входит. Поэтому «оплачено»
	// подтверждаем запросом счёта нашим токеном, если касса прислала номер счёта.
	if ev.Status == core.PayPaid && ev.ExternalID != "" {
		remote, err := g.Status(r.Context(), core.Payment{ID: ev.PaymentID, ExternalID: ev.ExternalID})
		if err != nil {
			return core.GatewayEvent{}, err
		}
		if remote.Status != core.PayPaid {
			return core.GatewayEvent{}, errors.New("PayPalych не подтвердила оплату из postback")
		}
	}
	return ev, nil
}

// postback читает поля из формы или из JSON, значения приводит к строкам.
func postback(r *http.Request) (map[string]string, error) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		return nil, errors.New("postback кассы PayPalych не прочитан")
	}
	out := map[string]string{}
	if strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		var raw map[string]any
		dec := json.NewDecoder(bytes.NewReader(body))
		dec.UseNumber()
		if err := dec.Decode(&raw); err != nil {
			return nil, errors.New("postback кассы PayPalych не разобран")
		}
		for k, v := range raw {
			if s, ok := v.(string); ok {
				out[k] = s
			} else if n, ok := v.(json.Number); ok {
				out[k] = n.String()
			}
		}
		return out, nil
	}
	form, err := url.ParseQuery(string(body))
	if err != nil {
		return nil, errors.New("postback кассы PayPalych не разобран")
	}
	for k := range form {
		out[k] = form.Get(k)
	}
	return out, nil
}

func mapStatus(s string) core.PayStatus {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case "SUCCESS", "OVERPAID":
		return core.PayPaid
	case "FAIL", "CANCELLED", "CANCELED":
		return core.PayFailed
	}
	return core.PayPending // NEW, PROCESS, UNDERPAID
}

// do шлёт запрос в API. Токен идёт только в заголовок и в ошибки не попадает.
func (g *Gateway) do(ctx context.Context, method, path string, query url.Values, in map[string]string, out any) error {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}
	target := g.baseURL + path
	if len(query) > 0 {
		target += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, target, body)
	if err != nil {
		return errors.New("не собран запрос к PayPalych")
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(g.cfg.Secret(keyToken)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := g.client.Do(req)
	if err != nil {
		return errors.New("касса PayPalych недоступна")
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("касса PayPalych ответила кодом %d", resp.StatusCode)
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(out); err != nil {
		return errors.New("ответ кассы PayPalych не разобран")
	}
	return nil
}
