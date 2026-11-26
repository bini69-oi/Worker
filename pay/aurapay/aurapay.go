// Package aurapay это касса AuraPay (app.aurapay.tech): счёт на оплату по СБП или картой по ссылке.
//
// Запросы идут с заголовками X-ApiKey и X-ShopId. Вебхук подписан вторым секретным ключом:
// заголовок X-SIGNATURE это HMAC-SHA256 от значений всех полей тела, склеенных в алфавитном порядке ключей.
package aurapay

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
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/bini69-oi/worker/core"
	"github.com/bini69-oi/worker/pay"
	"github.com/bini69-oi/worker/settings"
)

// DefaultBaseURL это адрес API AuraPay.
const DefaultBaseURL = "https://app.aurapay.tech"

const (
	keyAPI     = "aurapay.api_key"
	keyShop    = "aurapay.shop_id"
	keySecret  = "aurapay.secret_key"
	keyService = "aurapay.service"
	keyReturn  = "aurapay.return_url"
	keyURL     = "aurapay.hook_url"

	// orderPrefix метит наш номер платежа в order_id, чтобы узнать его в вебхуке.
	orderPrefix = "worker_"
	maxBody     = 1 << 20
)

func init() {
	settings.AddGateway("aurapay", "СБП и карты (AuraPay)",
		settings.Def{Key: keyAPI, Kind: settings.KindString, Secret: true, Group: "pay.aurapay",
			Desc: "API-ключ (X-ApiKey) из кабинета AuraPay"},
		settings.Def{Key: keyShop, Kind: settings.KindString, Secret: true, Group: "pay.aurapay",
			Desc: "ID магазина (X-ShopId, UUID) из кабинета AuraPay"},
		settings.Def{Key: keySecret, Kind: settings.KindString, Secret: true, Group: "pay.aurapay",
			Desc: "Секретный ключ №2: им AuraPay подписывает вебхуки"},
		settings.Def{Key: keyService, Kind: settings.KindString, Group: "pay.aurapay", Default: "sbp",
			Desc: "Способ оплаты в счёте: sbp или card"},
		settings.Def{Key: keyURL, Kind: settings.KindString, Group: "pay.aurapay",
			Desc: "Адрес вебхука для счетов: https://ваш-сайт/pay/aurapay. Пусто: подставит сборка"},
		settings.Def{Key: keyReturn, Kind: settings.KindString, Group: "pay.aurapay",
			Desc: "Куда вернуть абонента после оплаты, {payment} заменится номером платежа. Пусто: обратно в бота"},
	)
}

// Config это настройки, которые касса читает на лету.
type Config interface {
	String(key string) string
	Secret(key string) string
}

// Gateway это касса AuraPay.
type Gateway struct {
	cfg     Config
	baseURL string
	client  *http.Client
	// Return это адрес возврата по умолчанию, если настройка пустая. Ставит сборка: диплинк в бота.
	Return func(paymentID int64) string
	// Hook это адрес вебхука по умолчанию, если настройка пустая. Ставит сборка: PUBLIC_URL/pay/aurapay.
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

func (g *Gateway) Name() string { return "aurapay" }

func (g *Gateway) Configured() bool {
	return g.cfg.Secret(keyAPI) != "" && g.cfg.Secret(keyShop) != "" && g.cfg.Secret(keySecret) != ""
}

// invoice это счёт в ответах создания и проверки статуса.
type invoice struct {
	ID          string      `json:"id"`
	Status      string      `json:"status"`
	Amount      json.Number `json:"amount"`
	PaymentData struct {
		URL string `json:"url"`
	} `json:"payment_data"`
}

func orderID(id int64) string { return orderPrefix + strconv.FormatInt(id, 10) }

func (g *Gateway) Create(ctx context.Context, p core.Payment) (core.Invoice, error) {
	service := strings.TrimSpace(g.cfg.String(keyService))
	if service == "" {
		service = "sbp"
	}
	req := map[string]any{
		"amount":   json.Number(rubles(p.Amount)),
		"order_id": orderID(p.ID),
		"comment":  fmt.Sprintf("Пополнение %d", p.ID),
		"service":  service,
	}
	hook := strings.TrimSpace(g.cfg.String(keyURL))
	if hook == "" {
		hook = g.Hook
	}
	if hook != "" {
		req["callback_url"] = hook
	}
	if back := pay.ReturnURL(g.cfg.String(keyReturn), g.Return, p.ID); back != "" {
		req["success_url"], req["fail_url"] = back, back
	}
	var res invoice
	if err := g.call(ctx, "/invoice/create", req, &res); err != nil {
		return core.Invoice{}, err
	}
	if res.ID == "" || res.PaymentData.URL == "" {
		return core.Invoice{}, errors.New("касса AuraPay не вернула номер счёта или ссылку")
	}
	return core.Invoice{ExternalID: res.ID, URL: res.PaymentData.URL, ExtAmount: p.Amount, ExtCurrency: "RUB"}, nil
}

func (g *Gateway) Status(ctx context.Context, p core.Payment) (core.GatewayEvent, error) {
	req := map[string]any{}
	if p.ExternalID != "" {
		req["id"] = p.ExternalID
	} else {
		req["order_id"] = orderID(p.ID)
	}
	var res invoice
	if err := g.call(ctx, "/invoice/status", req, &res); err != nil {
		return core.GatewayEvent{}, err
	}
	ev := core.GatewayEvent{PaymentID: p.ID, ExternalID: p.ExternalID, Status: mapStatus(res.Status)}
	if ev.Status != core.PayPaid {
		return ev, nil
	}
	var err error
	ev.ExtAmount, err = amountOf(res.Amount)
	return ev, err
}

// Verify сверяет X-SIGNATURE с HMAC второго ключа от склеенных значений тела и разбирает вебхук.
func (g *Gateway) Verify(r *http.Request) (core.GatewayEvent, error) {
	key := g.cfg.Secret(keySecret)
	body, err := io.ReadAll(io.LimitReader(r.Body, maxBody))
	if err != nil {
		return core.GatewayEvent{}, errors.New("вебхук кассы AuraPay не прочитан")
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	var fields map[string]any
	if err := dec.Decode(&fields); err != nil {
		return core.GatewayEvent{}, pay.ErrBadSignature
	}
	got, err := hex.DecodeString(strings.TrimSpace(r.Header.Get("X-SIGNATURE")))
	msg, ok := concat(fields)
	if key == "" || err != nil || !ok || !hmac.Equal(got, sign([]byte(msg), key)) {
		return core.GatewayEvent{}, pay.ErrBadSignature
	}
	ev := core.GatewayEvent{ExternalID: text(fields["id"]), Status: mapStatus(text(fields["status"]))}
	if n, ok := strings.CutPrefix(text(fields["order_id"]), orderPrefix); ok {
		ev.PaymentID, _ = strconv.ParseInt(n, 10, 64)
	}
	if ev.ExternalID == "" {
		return ev, errors.New("в вебхуке кассы AuraPay нет номера счёта")
	}
	if ev.Status == core.PayPaid {
		ev.ExtAmount, err = amountOf(json.Number(text(fields["amount"])))
	}
	return ev, err
}

// concat склеивает значения полей в алфавитном порядке ключей. Поле signature, если оно есть, не входит.
// Вложенные объекты и списки подписать нечем: ok=false.
func concat(fields map[string]any) (string, bool) {
	keys := make([]string, 0, len(fields))
	for k := range fields {
		if k != "signature" {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	var sb strings.Builder
	for _, k := range keys {
		switch fields[k].(type) {
		case map[string]any, []any:
			return "", false
		}
		sb.WriteString(text(fields[k]))
	}
	return sb.String(), true
}

// text это значение поля как строка: null пустой, bool как True и False (так склеивает касса).
func text(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case json.Number:
		return t.String()
	case bool:
		if t {
			return "True"
		}
		return "False"
	}
	return ""
}

// amountOf переводит сумму кассы в копейки. Без суммы зачислять нечего.
func amountOf(n json.Number) (int64, error) {
	if n == "" {
		return 0, errors.New("в ответе AuraPay нет суммы")
	}
	return kopecks(string(n))
}

func mapStatus(s string) core.PayStatus {
	switch strings.ToUpper(s) {
	case "PAID":
		return core.PayPaid
	case "EXPIRED", "CANCELLED", "CANCELED", "FAILED":
		return core.PayFailed
	}
	return core.PayPending
}

// sign это HMAC-SHA256 от байт сообщения.
func sign(msg []byte, key string) []byte {
	m := hmac.New(sha256.New, []byte(key))
	m.Write(msg)
	return m.Sum(nil)
}

// call шлёт запрос. Ключи идут только в заголовки, в ошибки не попадают.
func (g *Gateway) call(ctx context.Context, path string, in map[string]any, out *invoice) error {
	body, err := json.Marshal(in)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return errors.New("не собран запрос к AuraPay")
	}
	req.Header.Set("X-ApiKey", g.cfg.Secret(keyAPI))
	req.Header.Set("X-ShopId", g.cfg.Secret(keyShop))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := g.client.Do(req)
	if err != nil {
		return errors.New("касса AuraPay недоступна")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("касса AuraPay ответила кодом %d", resp.StatusCode)
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxBody)).Decode(out); err != nil {
		return errors.New("ответ кассы AuraPay не разобран")
	}
	return nil
}
