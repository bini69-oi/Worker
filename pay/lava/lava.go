// Package lava это касса Lava (lava.ru, бизнес-API): счёт на оплату картой или СБП по ссылке.
//
// Запросы подписаны HMAC-SHA256 от точных байт тела секретным ключом (заголовок Signature).
// Вебхук подписан дополнительным ключом так же и приходит в заголовке Authorization.
package lava

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
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/bini69-oi/worker/core"
	"github.com/bini69-oi/worker/pay"
	"github.com/bini69-oi/worker/settings"
)

// DefaultBaseURL это адрес API Lava.
const DefaultBaseURL = "https://api.lava.ru"

const (
	keyShop   = "lava.shop_id"
	keySecret = "lava.secret_key"
	keyHook   = "lava.hook_key"
	keyReturn = "lava.return_url"
	keyURL    = "lava.hook_url"

	// orderPrefix метит наш номер платежа в orderId, чтобы узнать его в вебхуке.
	orderPrefix = "worker_"
	maxBody     = 1 << 20
)

func init() {
	settings.AddGateway("lava", "СБП и карты (Lava)",
		settings.Def{Key: keyShop, Kind: settings.KindString, Secret: true, Group: "pay.lava",
			Desc: "ID проекта (shop id) из кабинета Lava"},
		settings.Def{Key: keySecret, Kind: settings.KindString, Secret: true, Group: "pay.lava",
			Desc: "Секретный ключ проекта: подписывает наши запросы"},
		settings.Def{Key: keyHook, Kind: settings.KindString, Secret: true, Group: "pay.lava",
			Desc: "Дополнительный ключ проекта: им Lava подписывает вебхуки"},
		settings.Def{Key: keyURL, Kind: settings.KindString, Group: "pay.lava",
			Desc: "Адрес вебхука для счетов: https://ваш-сайт/pay/lava. Пусто: подставит сборка"},
		settings.Def{Key: keyReturn, Kind: settings.KindString, Group: "pay.lava",
			Desc: "Куда вернуть абонента после оплаты, {payment} заменится номером платежа. Запрос после ? Lava не принимает. Пусто: обратно в бота"},
	)
}

// Config это настройки, которые касса читает на лету.
type Config interface {
	String(key string) string
	Secret(key string) string
}

// Gateway это касса Lava.
type Gateway struct {
	cfg     Config
	baseURL string
	client  *http.Client
	// Return это адрес возврата по умолчанию, если настройка пустая. Ставит сборка: диплинк в бота.
	Return func(paymentID int64) string
	// Hook это адрес вебхука по умолчанию, если настройка пустая. Ставит сборка: PUBLIC_URL/pay/lava.
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

func (g *Gateway) Name() string { return "lava" }

func (g *Gateway) Configured() bool {
	return g.cfg.Secret(keyShop) != "" && g.cfg.Secret(keySecret) != "" && g.cfg.Secret(keyHook) != ""
}

// reply это ответ API: полезные данные лежат в data.
type reply struct {
	Status any     `json:"status"`
	Error  any     `json:"error"`
	Data   invoice `json:"data"`
}

// invoice это счёт в ответах создания и проверки статуса.
type invoice struct {
	ID     string      `json:"id"`
	URL    string      `json:"url"`
	Status string      `json:"status"`
	Amount json.Number `json:"amount"`
}

func (g *Gateway) Create(ctx context.Context, p core.Payment) (core.Invoice, error) {
	req := map[string]any{
		"shopId":  g.cfg.Secret(keyShop),
		"orderId": orderPrefix + strconv.FormatInt(p.ID, 10),
		"sum":     json.Number(pay.Rubles(p.Amount)),
		"comment": fmt.Sprintf("Пополнение %d", p.ID),
	}
	hook := strings.TrimSpace(g.cfg.String(keyURL))
	if hook == "" {
		hook = g.Hook
	}
	if hook != "" {
		req["hookUrl"] = hook
	}
	if back := cleanURL(pay.ReturnURL(g.cfg.String(keyReturn), g.Return, p.ID)); back != "" {
		req["successUrl"], req["failUrl"] = back, back
	}
	var res reply
	if err := g.call(ctx, "/business/invoice/create", req, &res); err != nil {
		return core.Invoice{}, err
	}
	if res.Data.ID == "" || res.Data.URL == "" {
		return core.Invoice{}, errors.New("касса Lava не вернула номер счёта или ссылку")
	}
	return core.Invoice{ExternalID: res.Data.ID, URL: res.Data.URL, ExtAmount: p.Amount, ExtCurrency: "RUB"}, nil
}

func (g *Gateway) Status(ctx context.Context, p core.Payment) (core.GatewayEvent, error) {
	req := map[string]any{"shopId": g.cfg.Secret(keyShop)}
	if p.ExternalID != "" {
		req["invoiceId"] = p.ExternalID
	} else {
		req["orderId"] = orderPrefix + strconv.FormatInt(p.ID, 10)
	}
	var res reply
	if err := g.call(ctx, "/business/invoice/status", req, &res); err != nil {
		return core.GatewayEvent{}, err
	}
	ev := core.GatewayEvent{PaymentID: p.ID, ExternalID: p.ExternalID, Status: mapStatus(res.Data.Status)}
	if ev.Status != core.PayPaid {
		return ev, nil
	}
	var err error
	ev.ExtAmount, err = amountOf(res.Data.Amount)
	return ev, err
}

// hookBody это поля вебхука Lava.
type hookBody struct {
	InvoiceID string      `json:"invoice_id"`
	OrderID   string      `json:"order_id"`
	Status    string      `json:"status"`
	Amount    json.Number `json:"amount"`
}

// Verify сверяет подпись из заголовка Authorization с HMAC дополнительного ключа
// от точных байт тела, как их подписала касса. Тело не пересобирается.
func (g *Gateway) Verify(r *http.Request) (core.GatewayEvent, error) {
	key := g.cfg.Secret(keyHook)
	body, err := io.ReadAll(io.LimitReader(r.Body, maxBody))
	if err != nil {
		return core.GatewayEvent{}, errors.New("вебхук кассы Lava не прочитан")
	}
	got, err := hex.DecodeString(strings.TrimSpace(r.Header.Get("Authorization")))
	if key == "" || err != nil || !hmac.Equal(got, sign(body, key)) {
		return core.GatewayEvent{}, pay.ErrBadSignature
	}
	var h hookBody
	if err := json.Unmarshal(body, &h); err != nil {
		return core.GatewayEvent{}, errors.New("вебхук кассы Lava не разобран")
	}
	ev := core.GatewayEvent{ExternalID: h.InvoiceID, Status: mapStatus(h.Status)}
	if n, ok := strings.CutPrefix(h.OrderID, orderPrefix); ok {
		ev.PaymentID, _ = strconv.ParseInt(n, 10, 64)
	}
	if ev.ExternalID == "" {
		return ev, errors.New("в вебхуке кассы Lava нет номера счёта")
	}
	if ev.Status == core.PayPaid {
		ev.ExtAmount, err = amountOf(h.Amount)
	}
	return ev, err
}

// amountOf переводит сумму кассы в копейки. Без суммы зачислять нечего.
func amountOf(n json.Number) (int64, error) {
	if n == "" {
		return 0, errors.New("в ответе Lava нет суммы")
	}
	return pay.Kopecks(string(n))
}

func mapStatus(s string) core.PayStatus {
	switch strings.ToLower(s) {
	case "success", "paid":
		return core.PayPaid
	case "cancel", "cancelled", "canceled", "expired", "error", "failed":
		return core.PayFailed
	}
	return core.PayPending
}

// cleanURL убирает из адреса возврата запрос и якорь: Lava отвергает ссылки с ними.
func cleanURL(s string) string {
	u, err := url.Parse(s)
	if err != nil || u.Scheme == "" {
		return ""
	}
	u.RawQuery, u.Fragment = "", ""
	return u.String()
}

// sign это HMAC-SHA256 от байт тела.
func sign(body []byte, key string) []byte {
	m := hmac.New(sha256.New, []byte(key))
	m.Write(body)
	return m.Sum(nil)
}

// call шлёт подписанный запрос. Ключи идут только в тело и заголовок, в ошибки не попадают.
func (g *Gateway) call(ctx context.Context, path string, in map[string]any, out *reply) error {
	body, err := json.Marshal(in)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return errors.New("не собран запрос к Lava")
	}
	req.Header.Set("Signature", hex.EncodeToString(sign(body, g.cfg.Secret(keySecret))))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := g.client.Do(req)
	if err != nil {
		return errors.New("касса Lava недоступна")
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("касса Lava ответила кодом %d", resp.StatusCode)
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxBody)).Decode(out); err != nil {
		return errors.New("ответ кассы Lava не разобран")
	}
	if s, ok := out.Status.(string); ok && strings.EqualFold(s, "error") {
		return errors.New("касса Lava вернула ошибку")
	}
	return nil
}
