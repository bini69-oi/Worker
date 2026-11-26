// Package heleket это касса Heleket: крипто-счёт, выставленный в рублях.
// Курс считает сама касса: мы шлём рубли и желаемую монету (to_currency), Heleket пересчитывает.
// Поэтому сумма счёта, вебхука и сверки всегда в рублях, своего курса у нас нет.
package heleket

import (
	"bytes"
	"context"
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

// DefaultBaseURL это адрес API Heleket.
const DefaultBaseURL = "https://api.heleket.com/v1"

const (
	keyMerchant = "heleket.merchant"
	keyAPIKey   = "heleket.api_key"
	keyCoin     = "heleket.to_currency"
	keyNetwork  = "heleket.network"
	keyLifetime = "heleket.lifetime"
	keyCallback = "heleket.callback_url"
	keyReturn   = "heleket.return_url"

	// payloadPrefix метит наш номер в order_id. Heleket допускает в order_id только буквы, цифры, "_" и "-".
	payloadPrefix = "worker_"
	maxBody       = 1 << 20
)

func init() {
	settings.AddGateway("heleket", "Heleket (крипта)",
		settings.Def{Key: keyMerchant, Kind: settings.KindString, Secret: true, Group: "pay.heleket",
			Desc: "UUID мерчанта из кабинета Heleket (Настройки, Мерчант)"},
		settings.Def{Key: keyAPIKey, Kind: settings.KindString, Secret: true, Group: "pay.heleket",
			Desc: "API-ключ приёма платежей (Payment API key) из кабинета Heleket"},
		settings.Def{Key: keyCoin, Kind: settings.KindString, Default: "USDT", Group: "pay.heleket",
			Desc: "Монета, в которую Heleket пересчитает рубли, например USDT. Пусто: абонент выбирает сам"},
		settings.Def{Key: keyNetwork, Kind: settings.KindString, Group: "pay.heleket",
			Desc: "Сеть для монеты, например tron. Пусто: абонент выбирает сам"},
		settings.Def{Key: keyLifetime, Kind: settings.KindInt, Default: "3600", Group: "pay.heleket",
			Desc: "Сколько секунд счёт ждёт оплаты (от 300 до 43200)"},
		settings.Def{Key: keyCallback, Kind: settings.KindString, Group: "pay.heleket",
			Desc: "Адрес вебхука для счетов: https://ваш-сайт/pay/heleket. Пусто: оплата подтянется проверкой статуса"},
		settings.Def{Key: keyReturn, Kind: settings.KindString, Group: "pay.heleket",
			Desc: "Куда вернуть абонента после оплаты, {payment} заменится номером платежа. Пусто: обратно в бота"},
	)
}

// Config это настройки, которые касса читает на лету.
type Config interface {
	String(key string) string
	Int(key string) int64
	Secret(key string) string
}

// Gateway это касса Heleket.
type Gateway struct {
	cfg     Config
	baseURL string
	client  *http.Client
	// Return это адрес возврата по умолчанию, если настройка пустая. Ставит сборка: диплинк в бота.
	Return func(paymentID int64) string
	// Hook это адрес вебхука по умолчанию, если настройка пустая. Ставит сборка: PUBLIC_URL/pay/<касса>.
	Hook string
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
	return &Gateway{cfg: cfg, baseURL: strings.TrimRight(baseURL, "/"), client: client}
}

func (g *Gateway) Name() string { return "heleket" }

func (g *Gateway) Configured() bool {
	return g.cfg.Secret(keyMerchant) != "" && g.cfg.Secret(keyAPIKey) != ""
}

// payment это счёт в ответе API и в вебхуке (нужные нам поля). Суммы приходят строками.
type payment struct {
	Type          string      `json:"type"`
	UUID          string      `json:"uuid"`
	OrderID       string      `json:"order_id"`
	Amount        json.Number `json:"amount"`
	Currency      string      `json:"currency"`
	Status        string      `json:"status"`
	PaymentStatus string      `json:"payment_status"`
	URL           string      `json:"url"`
}

func (p payment) status() string {
	if p.PaymentStatus != "" {
		return p.PaymentStatus
	}
	return p.Status
}

// mapStatus переводит статус Heleket в статус ядра. paid_over это оплата с переплатой, она в плюс.
// wrong_amount (недоплата) не зачисляем и не закрываем: абонент может доплатить, а сумму сверяет ядро.
func mapStatus(s string) core.PayStatus {
	switch strings.ToLower(s) {
	case "paid", "paid_over":
		return core.PayPaid
	case "cancel", "fail", "system_fail":
		return core.PayFailed
	case "refund_paid":
		return core.PayRefunded
	}
	return core.PayPending // check, process, confirm_check, wrong_amount, locked, refund_process, refund_fail
}

// toEvent переводит счёт в событие. Сумму несёт только оплаченный: в рублях и строго по счёту.
func toEvent(p payment) (core.GatewayEvent, error) {
	ev := core.GatewayEvent{ExternalID: p.UUID, Status: mapStatus(p.status())}
	if n, ok := strings.CutPrefix(p.OrderID, payloadPrefix); ok {
		ev.PaymentID, _ = strconv.ParseInt(n, 10, 64)
	}
	if ev.Status != core.PayPaid {
		return ev, nil
	}
	if !strings.EqualFold(p.Currency, "RUB") {
		return core.GatewayEvent{}, fmt.Errorf("счёт Heleket выставлен не в рублях (%q)", p.Currency)
	}
	k, err := kopecks(string(p.Amount))
	if err != nil {
		return core.GatewayEvent{}, err
	}
	ev.ExtAmount = k
	return ev, nil
}

// Create выставляет счёт в рублях. Монету, сеть и срок берём из настроек.
func (g *Gateway) Create(ctx context.Context, p core.Payment) (core.Invoice, error) {
	req := map[string]any{
		"amount":   rubles(p.Amount),
		"currency": "RUB",
		"order_id": payloadPrefix + strconv.FormatInt(p.ID, 10),
		"lifetime": lifetime(g.cfg.Int(keyLifetime)),
	}
	if c := strings.ToUpper(strings.TrimSpace(g.cfg.String(keyCoin))); c != "" {
		req["to_currency"] = c
	}
	if n := strings.TrimSpace(g.cfg.String(keyNetwork)); n != "" {
		req["network"] = n
	}
	if u := strings.TrimSpace(g.cfg.String(keyCallback)); u != "" {
		req["url_callback"] = u
	} else if g.Hook != "" {
		req["url_callback"] = g.Hook
	}
	back := pay.ReturnURL(g.cfg.String(keyReturn), g.Return, p.ID)
	if back != "" {
		req["url_return"], req["url_success"] = back, back
	}
	var res payment
	if err := g.call(ctx, "payment", req, &res); err != nil {
		return core.Invoice{}, err
	}
	if res.UUID == "" || res.URL == "" {
		return core.Invoice{}, errors.New("касса Heleket не вернула счёт или ссылку")
	}
	return core.Invoice{ExternalID: res.UUID, URL: res.URL, ExtAmount: p.Amount, ExtCurrency: "RUB"}, nil
}

// lifetime держит срок счёта в пределах, которые принимает Heleket.
func lifetime(sec int64) int64 {
	if sec < 300 || sec > 43200 {
		return 3600
	}
	return sec
}

// Status спрашивает счёт у кассы по его номеру.
func (g *Gateway) Status(ctx context.Context, p core.Payment) (core.GatewayEvent, error) {
	var res payment
	if err := g.call(ctx, "payment/info", map[string]any{"uuid": p.ExternalID}, &res); err != nil {
		return core.GatewayEvent{}, err
	}
	if res.UUID != "" && res.UUID != p.ExternalID {
		return core.GatewayEvent{}, errors.New("касса Heleket ответила про другой счёт")
	}
	res.UUID = p.ExternalID
	return toEvent(res)
}

// Verify проверяет подпись вебхука и разбирает его. Подпись лежит в самом теле, в поле sign.
func (g *Gateway) Verify(r *http.Request) (core.GatewayEvent, error) {
	raw, err := io.ReadAll(io.LimitReader(r.Body, maxBody))
	if err != nil {
		return core.GatewayEvent{}, errors.New("тело вебхука Heleket не прочитано")
	}
	if !validSignature(raw, g.cfg.Secret(keyAPIKey)) {
		return core.GatewayEvent{}, pay.ErrBadSignature
	}
	var hook payment
	if err := json.Unmarshal(raw, &hook); err != nil {
		return core.GatewayEvent{}, errors.New("вебхук Heleket не разобран")
	}
	if hook.UUID == "" {
		return core.GatewayEvent{}, errors.New("в вебхуке Heleket нет номера счёта")
	}
	if hook.Type != "" && hook.Type != "payment" {
		// Выплаты и прочее нам не нужны: ответ 200 без зачисления.
		return core.GatewayEvent{ExternalID: hook.UUID, Status: core.PayPending}, nil
	}
	return toEvent(hook)
}

type apiReply struct {
	State  int             `json:"state"`
	Result json.RawMessage `json:"result"`
}

// call зовёт метод API. Ключ идёт только в подпись и в ошибки не попадает.
func (g *Gateway) call(ctx context.Context, name string, in map[string]any, out any) error {
	merchant, key := g.cfg.Secret(keyMerchant), g.cfg.Secret(keyAPIKey)
	if merchant == "" || key == "" {
		return errors.New("касса Heleket не настроена")
	}
	body, err := jsonBody(in)
	if err != nil {
		return fmt.Errorf("запрос Heleket %s не собран", name)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.baseURL+"/"+name, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("запрос Heleket %s не собран", name)
	}
	req.Header.Set("merchant", merchant)
	req.Header.Set("sign", sign(body, key))
	req.Header.Set("Content-Type", "application/json")
	resp, err := g.client.Do(req)
	if err != nil {
		// Ошибку транспорта не отдаём как есть: в ней может быть адрес запроса.
		return fmt.Errorf("касса Heleket не отвечает на %s", name)
	}
	defer resp.Body.Close()
	var reply apiReply
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxBody)).Decode(&reply); err != nil {
		return fmt.Errorf("ответ Heleket на %s не разобран (код %d)", name, resp.StatusCode)
	}
	if resp.StatusCode >= 300 || reply.State != 0 {
		return fmt.Errorf("касса Heleket отказала в %s (код %d, state %d)", name, resp.StatusCode, reply.State)
	}
	if err := json.Unmarshal(reply.Result, out); err != nil {
		return fmt.Errorf("ответ Heleket на %s не разобран", name)
	}
	return nil
}
