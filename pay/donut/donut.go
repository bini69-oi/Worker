// Package donut это касса Donut P2P (gw.donut.business): карта, СБП или СБП QR по ссылке.
//
// Каждый запрос и вебхук подписаны HMAC-SHA256 секретом магазина поверх строки «имя=значение» без разделителей.
// Ограничение: Donut не создаёт платёж без телефона плательщика, а ядро телефонов абонентов не хранит.
// Поэтому телефон берётся из секретной настройки donut.phone и один на всех. Без него касса создаёт счёт,
// только если Donut согласен на пустой телефон.
package donut

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
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

// DefaultBaseURL это адрес API Donut.
const DefaultBaseURL = "https://gw.donut.business"

const (
	keyToken  = "donut.token"
	keySecret = "donut.secret"
	keyPhone  = "donut.phone"
	keyMethod = "donut.method"
	keyID     = "donut.method_id"
	keyReturn = "donut.return_url"
	keyHook   = "donut.hook_url"

	// orderPrefix метит наш номер платежа в order_id.
	orderPrefix = "worker_"
	maxBody     = 1 << 20
)

func init() {
	settings.AddGateway("donut", "Карта и СБП (Donut)",
		settings.Def{Key: keyToken, Kind: settings.KindString, Secret: true, Group: "pay.donut",
			Desc: "Токен магазина из кабинета Donut"},
		settings.Def{Key: keySecret, Kind: settings.KindString, Secret: true, Group: "pay.donut",
			Desc: "Секрет магазина из кабинета Donut (ключ подписи запросов и вебхуков)"},
		settings.Def{Key: keyPhone, Kind: settings.KindString, Secret: true, Group: "pay.donut",
			Desc: "Телефон плательщика по умолчанию, 79001234567: Donut без него счёт не создаёт"},
		settings.Def{Key: keyMethod, Kind: settings.KindString, Group: "pay.donut", Default: "CARD",
			Desc: "Способ оплаты: CARD, SBP или SBP_QR"},
		settings.Def{Key: keyID, Kind: settings.KindString, Group: "pay.donut",
			Desc: "Номер метода Donut (method_id), если кабинет его выдал. Пусто: не отправлять"},
		settings.Def{Key: keyHook, Kind: settings.KindString, Group: "pay.donut",
			Desc: "Адрес вебхука для счетов: https://ваш-сайт/pay/donut. Пусто: подставит сборка"},
		settings.Def{Key: keyReturn, Kind: settings.KindString, Group: "pay.donut",
			Desc: "Куда вернуть абонента после оплаты, {payment} заменится номером платежа. Пусто: обратно в бота"},
	)
}

// Config это настройки, которые касса читает на лету.
type Config interface {
	String(key string) string
	Secret(key string) string
}

// Gateway это касса Donut.
type Gateway struct {
	cfg     Config
	baseURL string
	client  *http.Client
	// Return это адрес возврата по умолчанию, если настройка пустая. Ставит сборка: диплинк в бота.
	Return func(paymentID int64) string
	// Hook это адрес вебхука по умолчанию, если настройка пустая. Ставит сборка: PUBLIC_URL/pay/donut.
	Hook string
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

func (g *Gateway) Name() string { return "donut" }

func (g *Gateway) Configured() bool {
	return g.cfg.Secret(keyToken) != "" && g.cfg.Secret(keySecret) != ""
}

// scalar это значение поля, как оно записано в JSON: строка без кавычек, число или true/false. Нужно для подписи.
type scalar string

func (s *scalar) UnmarshalJSON(b []byte) error {
	t := strings.TrimSpace(string(b))
	switch {
	case t == "null":
		*s = ""
	case strings.HasPrefix(t, `"`):
		var v string
		if err := json.Unmarshal(b, &v); err != nil {
			return err
		}
		*s = scalar(v)
	default:
		*s = scalar(t)
	}
	return nil
}

// answer это общие поля ответа API и вебхука.
type answer struct {
	Token         scalar `json:"token"`
	TransactionID scalar `json:"transaction_id"`
	OrderID       scalar `json:"order_id"`
	Amount        struct {
		Value    scalar `json:"value"`
		Currency scalar `json:"currency"`
	} `json:"amount"`
	Recalculated scalar `json:"recalculated"`
	Signature    string `json:"signature"`
	RedirectURL  string `json:"redirect_url"`
	Details      struct {
		QR string `json:"qrcode_url"`
	} `json:"details"`
	Status struct {
		Type scalar `json:"type"`
	} `json:"status"`
}

// kopecks отдаёт сумму ответа: она обязана быть, валюта только рубли.
func (a answer) kopecks() (int64, error) {
	if c := string(a.Amount.Currency); c != "" && !strings.EqualFold(c, "RUB") {
		return 0, errors.New("платёж Donut не в рублях")
	}
	if a.Amount.Value == "" {
		return 0, errors.New("в ответе Donut нет суммы")
	}
	return kopecks(string(a.Amount.Value))
}

func (g *Gateway) Create(ctx context.Context, p core.Payment) (core.Invoice, error) {
	method := strings.ToUpper(strings.TrimSpace(g.cfg.String(keyMethod)))
	if method == "" {
		method = "CARD"
	}
	token := g.cfg.Secret(keyToken)
	order := orderPrefix + strconv.FormatInt(p.ID, 10)
	customer := map[string]string{"id": "u" + strconv.FormatInt(p.UserID, 10)}
	if phone := strings.TrimSpace(g.cfg.Secret(keyPhone)); phone != "" {
		customer["phone"] = phone
	}
	req := map[string]any{
		"token":       token,
		"order_id":    order,
		"amount":      map[string]string{"value": rubles(p.Amount), "currency": "RUB"},
		"customer":    customer,
		"redirect":    "true",
		"description": method,
	}
	if id := strings.TrimSpace(g.cfg.String(keyID)); id != "" {
		req["method_id"] = id
	}
	if back := pay.ReturnURL(g.cfg.String(keyReturn), g.Return, p.ID); back != "" {
		req["return_url"] = back
	}
	hook := strings.TrimSpace(g.cfg.String(keyHook))
	if hook == "" {
		hook = g.Hook
	}
	if hook != "" {
		req["callback_url"] = hook
	}
	req["signature"] = g.sign(
		"token", token, "order_id", order, "amount.value", rubles(p.Amount), "amount.currency", "RUB",
		"customer.id", customer["id"], "redirect", "true")
	res, err := g.do(ctx, "/p2p_payin", req)
	if err != nil {
		return core.Invoice{}, err
	}
	if mapStatus(string(res.Status.Type)) == core.PayFailed || res.Status.Type == "" {
		return core.Invoice{}, errors.New("касса Donut отклонила создание платежа")
	}
	link := res.RedirectURL
	if link == "" {
		link = res.Details.QR
	}
	if res.TransactionID == "" || link == "" {
		return core.Invoice{}, errors.New("касса Donut не вернула номер платежа или ссылку")
	}
	return core.Invoice{ExternalID: string(res.TransactionID), URL: link, ExtAmount: p.Amount, ExtCurrency: "RUB"}, nil
}

func (g *Gateway) Status(ctx context.Context, p core.Payment) (core.GatewayEvent, error) {
	token := g.cfg.Secret(keyToken)
	res, err := g.do(ctx, "/p2p_status", map[string]string{
		"token": token, "transaction_id": p.ExternalID,
		"signature": g.sign("token", token, "transaction_id", p.ExternalID),
	})
	if err != nil {
		return core.GatewayEvent{}, err
	}
	return event(p.ExternalID, res)
}

// event собирает событие из ответа: сумма нужна только у оплаченного платежа.
func event(id string, a answer) (core.GatewayEvent, error) {
	ev := core.GatewayEvent{ExternalID: id, Status: mapStatus(string(a.Status.Type))}
	var err error
	if ev.Status == core.PayPaid {
		ev.ExtAmount, err = a.kopecks()
	}
	return ev, err
}

// Verify проверяет подпись вебхука Donut (HMAC-SHA256 по полям вебхука) за постоянное время. Токен в вебхуке,
// если он есть, обязан совпасть с нашим. Деньги по неподписанному или чужому вебхуку не зачисляются.
func (g *Gateway) Verify(r *http.Request) (core.GatewayEvent, error) {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxBody))
	if err != nil {
		return core.GatewayEvent{}, errors.New("вебхук кассы Donut не прочитан")
	}
	var a answer
	if err := json.Unmarshal(body, &a); err != nil || a.Signature == "" || !g.Configured() {
		return core.GatewayEvent{}, pay.ErrBadSignature
	}
	if a.Token != "" && subtle.ConstantTimeCompare([]byte(a.Token), []byte(g.cfg.Secret(keyToken))) != 1 {
		return core.GatewayEvent{}, pay.ErrBadSignature
	}
	want := g.sign("token", string(a.Token), "transaction_id", string(a.TransactionID), "order_id", string(a.OrderID),
		"amount.value", string(a.Amount.Value), "amount.currency", string(a.Amount.Currency),
		"recalculated", string(a.Recalculated), "status.type", string(a.Status.Type))
	if !hmac.Equal([]byte(strings.ToLower(want)), []byte(strings.ToLower(strings.TrimSpace(a.Signature)))) {
		return core.GatewayEvent{}, pay.ErrBadSignature
	}
	if a.TransactionID == "" {
		return core.GatewayEvent{}, errors.New("в вебхуке Donut нет номера платежа")
	}
	return event(string(a.TransactionID), a)
}

// sign считает подпись: пары «имя, значение» склеиваются как имя=значение без разделителей, пустые пропускаются.
func (g *Gateway) sign(pairs ...string) string {
	var sb strings.Builder
	for i := 0; i+1 < len(pairs); i += 2 {
		if pairs[i+1] != "" {
			sb.WriteString(pairs[i] + "=" + pairs[i+1])
		}
	}
	m := hmac.New(sha256.New, []byte(g.cfg.Secret(keySecret)))
	m.Write([]byte(sb.String()))
	return hex.EncodeToString(m.Sum(nil))
}

func mapStatus(s string) core.PayStatus {
	switch strings.ToLower(s) {
	case "success":
		return core.PayPaid
	case "cancelled", "error":
		return core.PayFailed
	}
	return core.PayPending // created, processing
}

// do шлёт подписанный запрос в API. Ключи в ошибки не попадают.
func (g *Gateway) do(ctx context.Context, path string, in any) (answer, error) {
	b, err := json.Marshal(in)
	if err != nil {
		return answer{}, errors.New("запрос к Donut не собран")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.baseURL+path, bytes.NewReader(b))
	if err != nil {
		return answer{}, errors.New("запрос к Donut не собран")
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := g.client.Do(req)
	if err != nil {
		return answer{}, errors.New("касса Donut недоступна")
	}
	defer resp.Body.Close()
	var out answer
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxBody)).Decode(&out); err != nil || resp.StatusCode >= 300 {
		return answer{}, fmt.Errorf("касса Donut ответила кодом %d", resp.StatusCode)
	}
	return out, nil
}
