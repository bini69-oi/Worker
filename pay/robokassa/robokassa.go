// Package robokassa это касса Robokassa: оплата по подписанной ссылке, уведомление ResultURL,
// проверка статуса через OpState (XML).
//
// Ссылка: https://auth.robokassa.ru/Merchant/Index.aspx?MerchantLogin=..&OutSum=300&InvId=<наш номер платежа>&SignatureValue=..,
// подпись hash(логин:сумма:номер:пароль1). Уведомление: форма OutSum, InvId, SignatureValue,
// подпись hash(сумма:номер:пароль2), сумма берётся в той строке, что пришла. На него нужно ответить «OK<номер>».
// Адреса ResultURL, SuccessURL и FailURL задаются в кабинете Robokassa, в ссылку их не передать.
package robokassa

import (
	"context"
	"crypto/hmac"
	"crypto/md5"  // алгоритм по умолчанию задаёт сама касса, это не наш выбор
	"crypto/sha1" // то же: касса позволяет выбрать SHA-1
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"hash"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/bini69-oi/worker/core"
	"github.com/bini69-oi/worker/pay"
	"github.com/bini69-oi/worker/settings"
)

// DefaultBaseURL это сервис OpState, PayURL это страница оплаты.
const (
	DefaultBaseURL = "https://auth.robokassa.ru/Merchant/WebService/Service.asmx"
	PayURL         = "https://auth.robokassa.ru/Merchant/Index.aspx"

	keyLogin = "robokassa.login"
	keyPass1 = "robokassa.password1"
	keyPass2 = "robokassa.password2"
	keyHash  = "robokassa.hash"
	keyTest  = "robokassa.test"
)

func init() {
	settings.AddGateway("robokassa", "Карта и СБП (Robokassa)",
		settings.Def{Key: keyLogin, Kind: settings.KindString, Group: "pay.robokassa", Secret: true,
			Desc: "Идентификатор магазина (MerchantLogin) из кабинета Robokassa"},
		settings.Def{Key: keyPass1, Kind: settings.KindString, Group: "pay.robokassa", Secret: true,
			Desc: "Пароль #1 (подпись ссылки на оплату)"},
		settings.Def{Key: keyPass2, Kind: settings.KindString, Group: "pay.robokassa", Secret: true,
			Desc: "Пароль #2 (подпись уведомлений и проверки статуса)"},
		settings.Def{Key: keyHash, Kind: settings.KindString, Default: "md5", Group: "pay.robokassa",
			Desc: "Алгоритм подписи, как в кабинете: md5, sha1, sha256, sha384 или sha512"},
		settings.Def{Key: keyTest, Kind: settings.KindBool, Default: "false", Group: "pay.robokassa",
			Desc: "Тестовый режим (IsTest=1), в нём действуют тестовые пароли из кабинета"},
	)
}

// Config это настройки, из которых касса на лету читает ключи. Подходит *settings.Settings.
type Config interface {
	String(key string) string
	Bool(key string) bool
	Secret(key string) string
}

// Gateway это касса Robokassa.
type Gateway struct {
	cfg     Config
	baseURL string
	client  *http.Client
}

var (
	_ pay.Gateway = (*Gateway)(nil)
	_ pay.Acker   = (*Gateway)(nil)
)

// New собирает кассу. Пустой baseURL означает боевой сервис, nil вместо клиента означает клиент с таймаутом.
func New(cfg Config, baseURL string, client *http.Client) *Gateway {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	return &Gateway{cfg: cfg, baseURL: strings.TrimRight(baseURL, "/"), client: client}
}

func (g *Gateway) Name() string { return "robokassa" }

func (g *Gateway) Configured() bool {
	return g.cfg.Secret(keyLogin) != "" && g.cfg.Secret(keyPass1) != "" && g.cfg.Secret(keyPass2) != "" &&
		hasher(g.cfg.String(keyHash)) != nil
}

// hasher отдаёт конструктор хеша по имени из настройки; пусто значит md5. Неизвестное имя: nil.
func hasher(name string) func() hash.Hash {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "", "md5":
		return md5.New
	case "sha1":
		return sha1.New
	case "sha256":
		return sha256.New
	case "sha384":
		return sha512.New384
	case "sha512":
		return sha512.New
	}
	return nil
}

// sign считает подпись по строке из частей, склеенных двоеточием.
func (g *Gateway) sign(parts ...string) (string, error) {
	h := hasher(g.cfg.String(keyHash))
	if h == nil {
		return "", errors.New("неизвестный алгоритм подписи Robokassa")
	}
	w := h()
	_, _ = w.Write([]byte(strings.Join(parts, ":")))
	return hex.EncodeToString(w.Sum(nil)), nil
}

func (g *Gateway) Create(_ context.Context, p core.Payment) (core.Invoice, error) {
	login, p1 := g.cfg.Secret(keyLogin), g.cfg.Secret(keyPass1)
	if login == "" || p1 == "" {
		return core.Invoice{}, errors.New("касса Robokassa не настроена")
	}
	order, amount := strconv.FormatInt(p.ID, 10), formatMoney(p.Amount)
	sig, err := g.sign(login, amount, order, p1)
	if err != nil {
		return core.Invoice{}, err
	}
	q := url.Values{"MerchantLogin": {login}, "OutSum": {amount}, "InvId": {order},
		"SignatureValue": {sig}, "Culture": {"ru"}}
	if g.cfg.Bool(keyTest) {
		q.Set("IsTest", "1")
	}
	return core.Invoice{ExternalID: order, URL: PayURL + "?" + q.Encode(), ExtAmount: p.Amount, ExtCurrency: "RUB"}, nil
}

// Verify проверяет ResultURL: подпись за постоянное время без учёта регистра. Номер и сумму берём только из подписанной формы.
func (g *Gateway) Verify(r *http.Request) (core.GatewayEvent, error) {
	r.Body = http.MaxBytesReader(nil, r.Body, 1<<16)
	if err := r.ParseForm(); err != nil {
		return core.GatewayEvent{}, pay.ErrBadSignature
	}
	p2 := g.cfg.Secret(keyPass2)
	sum, order := r.Form.Get("OutSum"), r.Form.Get("InvId")
	if p2 == "" || sum == "" || order == "" {
		return core.GatewayEvent{}, pay.ErrBadSignature
	}
	parts := append([]string{sum, order, p2}, shpParams(r.Form)...)
	want, err := g.sign(parts...)
	if err != nil {
		return core.GatewayEvent{}, pay.ErrBadSignature
	}
	got := strings.TrimSpace(r.Form.Get("SignatureValue"))
	if !hmac.Equal([]byte(strings.ToLower(got)), []byte(strings.ToLower(want))) {
		return core.GatewayEvent{}, pay.ErrBadSignature
	}
	cents, err := pay.Kopecks(sum)
	id, err2 := strconv.ParseInt(order, 10, 64)
	if err != nil || err2 != nil || id <= 0 {
		return core.GatewayEvent{}, pay.ErrBadSignature
	}
	// Робокасса шлёт ResultURL только об оплаченном счёте.
	return core.GatewayEvent{PaymentID: id, ExternalID: order, ExtAmount: cents, Status: core.PayPaid}, nil
}

// shpParams собирает пользовательские поля Shp_* для подписи: "Shp_имя=значение" по алфавиту имён.
func shpParams(form url.Values) []string {
	var out []string
	for k := range form {
		if strings.HasPrefix(strings.ToLower(k), "shp_") {
			out = append(out, k+"="+form.Get(k))
		}
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i]) < strings.ToLower(out[j]) })
	return out
}

// Ack это ответ на ResultURL: слово OK и номер счёта, иначе касса повторяет уведомление.
func (g *Gateway) Ack(ev core.GatewayEvent) string { return "OK" + ev.ExternalID }

// opState это ответ OpState: код результата, код состояния счёта и сумма в валюте магазина.
type opState struct {
	Result struct {
		Code int `xml:"Code"`
	} `xml:"Result"`
	State struct {
		Code int `xml:"Code"`
	} `xml:"State"`
	Info struct {
		OutSum string `xml:"OutSum"`
	} `xml:"Info"`
}

// Status спрашивает счёт у OpState, подпись hash(логин:номер:пароль2).
func (g *Gateway) Status(ctx context.Context, p core.Payment) (core.GatewayEvent, error) {
	login, p2 := g.cfg.Secret(keyLogin), g.cfg.Secret(keyPass2)
	id, err := strconv.ParseInt(p.ExternalID, 10, 64)
	if login == "" || p2 == "" || err != nil || id <= 0 {
		return core.GatewayEvent{}, errors.New("нет ключей или номера платежа для запроса к Robokassa")
	}
	sig, err := g.sign(login, p.ExternalID, p2)
	if err != nil {
		return core.GatewayEvent{}, err
	}
	q := url.Values{"MerchantLogin": {login}, "InvoiceID": {p.ExternalID}, "Signature": {sig}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, g.baseURL+"/OpState?"+q.Encode(), nil)
	if err != nil {
		return core.GatewayEvent{}, errors.New("не собран запрос к Robokassa")
	}
	resp, err := g.client.Do(req)
	if err != nil {
		return core.GatewayEvent{}, errors.New("касса Robokassa недоступна") // текст ошибки сети не отдаём
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode/100 != 2 {
		return core.GatewayEvent{}, fmt.Errorf("касса Robokassa ответила кодом %d", resp.StatusCode)
	}
	return eventFromXML(raw, id)
}

// eventFromXML разбирает ответ OpState. Состояния: 100 оплачен, 10 отменён, 60 возврат, остальные ждут.
func eventFromXML(raw []byte, id int64) (core.GatewayEvent, error) {
	var st opState
	if err := xml.Unmarshal(raw, &st); err != nil {
		return core.GatewayEvent{}, errors.New("ответ Robokassa не разобран")
	}
	if st.Result.Code != 0 {
		return core.GatewayEvent{}, fmt.Errorf("касса Robokassa отказала в статусе, код %d", st.Result.Code)
	}
	ev := core.GatewayEvent{PaymentID: id, ExternalID: strconv.FormatInt(id, 10), Status: core.PayPending}
	switch st.State.Code {
	case 100:
		cents, err := pay.Kopecks(st.Info.OutSum)
		if err != nil {
			return core.GatewayEvent{}, errors.New("сумма счёта Robokassa не разобрана")
		}
		ev.Status, ev.ExtAmount = core.PayPaid, cents
	case 10:
		ev.Status = core.PayFailed
	case 60:
		ev.Status = core.PayRefunded
	}
	return ev, nil
}

// formatMoney переводит копейки в сумму для ссылки: целые рубли без дроби ("300"), иначе "300.50".
func formatMoney(cents int64) string {
	if cents%100 == 0 {
		return strconv.FormatInt(cents/100, 10)
	}
	return fmt.Sprintf("%d.%02d", cents/100, cents%100)
}
