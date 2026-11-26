// Package yoomoney это касса ЮMoney: кошелёк, оплата через форму Quickpay и HTTP-уведомления.
package yoomoney

import (
	"context"
	"crypto/sha1" // sha1 задан официальной документацией ЮMoney, это не наш выбор
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

// DefaultBaseURL это адрес ЮMoney: форма Quickpay и API истории операций.
const DefaultBaseURL = "https://yoomoney.ru"

// labelPrefix метит наш номер платежа в поле label, по нему платёж находят и в уведомлении, и в API.
const labelPrefix = "worker-"

// rubleCode это код рубля в уведомлении (ISO 4217).
const rubleCode = "643"

func init() {
	settings.AddGateway("yoomoney", "Кошелёк ЮMoney",
		settings.Def{Key: "yoomoney.wallet", Kind: settings.KindString, Secret: true, Group: "pay.yoomoney",
			Desc: "Номер кошелька ЮMoney, куда приходят деньги"},
		settings.Def{Key: "yoomoney.notification_secret", Kind: settings.KindString, Secret: true, Group: "pay.yoomoney",
			Desc: "Секрет для HTTP-уведомлений из настроек кошелька ЮMoney"},
		settings.Def{Key: "yoomoney.token", Kind: settings.KindString, Secret: true, Group: "pay.yoomoney",
			Desc: "Токен API с правом operation-history. Без него потерянное уведомление не наверстать"},
		settings.Def{Key: "yoomoney.payment_type", Kind: settings.KindString, Default: "AC", Group: "pay.yoomoney",
			Desc: "Способ в форме Quickpay: AC это банковская карта, PC это кошелёк ЮMoney"},
		settings.Def{Key: "yoomoney.return_url", Kind: settings.KindString, Group: "pay.yoomoney",
			Desc: "Куда вернуть абонента после оплаты, {payment} заменится номером платежа. Пусто: обратно в бота"},
	)
}

// Config это настройки, которые касса читает на лету.
type Config interface {
	String(key string) string
	Secret(key string) string
}

// Gateway это касса ЮMoney.
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

func (g *Gateway) Name() string { return "yoomoney" }

// Configured требует кошелёк и секрет уведомлений: без секрета нечем проверять оплату.
func (g *Gateway) Configured() bool {
	return g.cfg.Secret("yoomoney.wallet") != "" && g.cfg.Secret("yoomoney.notification_secret") != ""
}

func labelOf(paymentID int64) string { return labelPrefix + strconv.FormatInt(paymentID, 10) }

// paymentOf достаёт наш номер платежа из label. Второе значение: label наш.
func paymentOf(label string) (int64, bool) {
	n, ok := strings.CutPrefix(label, labelPrefix)
	if !ok {
		return 0, false
	}
	id, err := strconv.ParseInt(n, 10, 64)
	return id, err == nil && id > 0
}

// Create собирает ссылку на форму Quickpay. В API кассы счёт не создаётся: номером платежа служит label.
// Сумма sum это то, что спишется с плательщика, поэтому с ней потом сверяется withdraw_amount.
func (g *Gateway) Create(_ context.Context, p core.Payment) (core.Invoice, error) {
	wallet := g.cfg.Secret("yoomoney.wallet")
	if wallet == "" {
		return core.Invoice{}, errors.New("касса ЮMoney не настроена")
	}
	q := url.Values{}
	q.Set("receiver", wallet)
	q.Set("quickpay-form", "shop")
	q.Set("paymentType", paymentType(g.cfg.String("yoomoney.payment_type")))
	q.Set("sum", rubles(p.Amount))
	q.Set("label", labelOf(p.ID))
	q.Set("targets", fmt.Sprintf("Пополнение %d", p.ID))
	if back := pay.ReturnURL(g.cfg.String("yoomoney.return_url"), g.Return, p.ID); back != "" {
		q.Set("successURL", back)
	}
	return core.Invoice{ExternalID: labelOf(p.ID), URL: g.baseURL + "/quickpay/confirm?" + q.Encode(),
		ExtAmount: p.Amount, ExtCurrency: "RUB"}, nil
}

// paymentType пропускает только известные способы, иначе карту.
func paymentType(s string) string {
	if s == "PC" {
		return "PC"
	}
	return "AC"
}

// Status спрашивает историю операций по label. Без токена API отвечает «ждём»: оплату принесёт уведомление.
func (g *Gateway) Status(ctx context.Context, p core.Payment) (core.GatewayEvent, error) {
	label := labelOf(p.ID)
	ev := core.GatewayEvent{PaymentID: p.ID, ExternalID: label, Status: core.PayPending}
	token := g.cfg.Secret("yoomoney.token")
	if token == "" {
		return ev, nil
	}
	ops, err := g.history(ctx, token, label)
	if err != nil {
		return ev, err
	}
	for _, op := range ops {
		if op.Label != label || op.Direction != "in" || op.Status != "success" {
			continue
		}
		// В истории amount это сумма на кошельке получателя, то есть за вычетом комиссии карты. Сверяется как есть:
		// не сошлась, значит ядро не зачислит, и админ получит тревогу. Подгонять сумму под счёт нельзя.
		if ev.ExtAmount, err = kopecks(string(op.Amount)); err != nil {
			return ev, err
		}
		ev.Status = core.PayPaid
		return ev, nil
	}
	return ev, nil
}

type operation struct {
	Label     string      `json:"label"`
	Status    string      `json:"status"`
	Direction string      `json:"direction"`
	Amount    json.Number `json:"amount"`
}

// history зовёт operation-history. Токен идёт только в заголовок и в ошибки не попадает.
func (g *Gateway) history(ctx context.Context, token, label string) ([]operation, error) {
	form := url.Values{"label": {label}, "type": {"deposition"}, "records": {"10"}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.baseURL+"/api/operation-history",
		strings.NewReader(form.Encode()))
	if err != nil {
		return nil, errors.New("не собран запрос к ЮMoney")
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := g.client.Do(req)
	if err != nil {
		return nil, errors.New("ЮMoney недоступна")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ЮMoney ответила кодом %d", resp.StatusCode)
	}
	var out struct {
		Operations []operation `json:"operations"`
		Error      string      `json:"error"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&out); err != nil {
		return nil, errors.New("ответ ЮMoney не разобран")
	}
	if out.Error != "" {
		return nil, fmt.Errorf("ЮMoney вернула ошибку %q", out.Error)
	}
	return out.Operations, nil
}

// Verify проверяет sha1_hash уведомления по официальной документации ЮMoney:
// sha1 от notification_type&operation_id&amount&currency&datetime&sender&codepro&notification_secret&label.
// Деньги зачисляются по withdraw_amount: это сумма, списанная с плательщика, она равна sum из ссылки.
// Поле amount это сумма на кошельке после комиссии, она меньше счёта и для сверки не годится.
func (g *Gateway) Verify(r *http.Request) (core.GatewayEvent, error) {
	secret := g.cfg.Secret("yoomoney.notification_secret")
	if secret == "" {
		return core.GatewayEvent{}, pay.ErrBadSignature
	}
	r.Body = http.MaxBytesReader(nil, r.Body, 1<<16)
	if err := r.ParseForm(); err != nil {
		return core.GatewayEvent{}, errors.New("уведомление ЮMoney не разобрано")
	}
	f := r.PostForm
	want := signature(f, secret)
	got := strings.ToLower(strings.TrimSpace(f.Get("sha1_hash")))
	if got == "" || subtle.ConstantTimeCompare([]byte(got), []byte(want)) != 1 {
		return core.GatewayEvent{}, pay.ErrBadSignature
	}
	return parse(f)
}

// signature считает sha1 строки из документации. Пустые поля остаются пустыми, порядок фиксирован.
func signature(f url.Values, secret string) string {
	parts := []string{f.Get("notification_type"), f.Get("operation_id"), f.Get("amount"), f.Get("currency"),
		f.Get("datetime"), f.Get("sender"), f.Get("codepro"), secret, f.Get("label")}
	sum := sha1.Sum([]byte(strings.Join(parts, "&")))
	return hex.EncodeToString(sum[:])
}

// parse разбирает уже проверенное уведомление. Всё, что не готовая оплата, отдаёт как «ждём».
func parse(f url.Values) (core.GatewayEvent, error) {
	if t := f.Get("notification_type"); t != "p2p-incoming" && t != "card-incoming" {
		return core.GatewayEvent{}, errors.New("уведомление ЮMoney неизвестного типа")
	}
	if f.Get("currency") != rubleCode {
		return core.GatewayEvent{}, errors.New("уведомление ЮMoney не в рублях")
	}
	id, ok := paymentOf(f.Get("label"))
	if !ok {
		return core.GatewayEvent{}, errors.New("в уведомлении ЮMoney нет нашего label")
	}
	ev := core.GatewayEvent{PaymentID: id, ExternalID: labelOf(id), Status: core.PayPending}
	// Тестовое уведомление, перевод с кодом протекции и непринятый перевод деньгами ещё не стали.
	if f.Get("test_notification") == "true" || f.Get("codepro") == "true" || f.Get("unaccepted") == "true" {
		return ev, nil
	}
	// withdraw_amount есть не во всех уведомлениях. Без него берём amount: у перевода из кошелька комиссии нет,
	// а при комиссии сумма меньше счёта, и ядро отклонит зачисление, но не зачислит лишнего.
	raw := f.Get("withdraw_amount")
	if raw == "" {
		raw = f.Get("amount")
	}
	k, err := kopecks(raw)
	if err != nil {
		return ev, err
	}
	ev.ExtAmount, ev.Status = k, core.PayPaid
	return ev, nil
}

// rubles превращает копейки в строку рублей без float: 12345 это "123.45".
func rubles(kop int64) string {
	return fmt.Sprintf("%d.%02d", kop/100, kop%100)
}

// kopecks разбирает рубли вида "300", "300.5" или "300.00" в копейки без float.
func kopecks(s string) (int64, error) {
	whole, frac, _ := strings.Cut(strings.TrimSpace(s), ".")
	if len(frac) > 2 {
		if strings.Trim(frac[2:], "0") != "" {
			return 0, errors.New("сумма ЮMoney с долями копейки")
		}
		frac = frac[:2]
	}
	frac += strings.Repeat("0", 2-len(frac))
	w, err := strconv.ParseInt(whole, 10, 64)
	if err != nil || w < 0 {
		return 0, errors.New("сумма ЮMoney не разобрана")
	}
	f, err := strconv.ParseInt(frac, 10, 64)
	if err != nil || f < 0 || w == 0 && f == 0 {
		return 0, errors.New("сумма ЮMoney не разобрана")
	}
	return w*100 + f, nil
}
