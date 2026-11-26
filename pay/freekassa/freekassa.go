// Package freekassa это касса FreeKassa: оплата по ссылке формы (SCI), уведомление с подписью MD5,
// проверка статуса через API v1 (подпись HMAC-SHA256).
//
// Ссылка: https://pay.fk.money/?m=<магазин>&oa=<сумма>&currency=RUB&o=<наш номер платежа>&s=<подпись>,
// подпись MD5(магазин:сумма:секрет1:RUB:номер).
// Уведомление: POST-форма MERCHANT_ID, AMOUNT, MERCHANT_ORDER_ID, intid, SIGN,
// подпись MD5(магазин:сумма:секрет2:номер), сумма подписывается ровно в той строке, что пришла.
package freekassa

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/md5" //nolint:gosec // MD5 требует сама касса, это не наш выбор
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
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

// DefaultBaseURL это адрес API FreeKassa v1, PayURL это форма оплаты.
const (
	DefaultBaseURL = "https://api.fk.life/v1"
	PayURL         = "https://pay.fk.money/"
)

func init() {
	settings.AddGateway("freekassa", "Карта и СБП (FreeKassa)",
		settings.Def{Key: "freekassa.shop_id", Kind: settings.KindString, Group: "pay.freekassa", Secret: true,
			Desc: "Номер магазина в FreeKassa"},
		settings.Def{Key: "freekassa.secret1", Kind: settings.KindString, Group: "pay.freekassa", Secret: true,
			Desc: "Секретное слово 1 (подпись ссылки на оплату)"},
		settings.Def{Key: "freekassa.secret2", Kind: settings.KindString, Group: "pay.freekassa", Secret: true,
			Desc: "Секретное слово 2 (подпись уведомлений)"},
		settings.Def{Key: "freekassa.api_key", Kind: settings.KindString, Group: "pay.freekassa", Secret: true,
			Desc: "API-ключ FreeKassa, нужен для сверки статуса потерянных платежей. Пусто: сверки нет"},
	)
}

// Config это настройки, из которых касса на лету читает ключи. Подходит *settings.Settings.
type Config interface {
	Secret(key string) string
}

// Gateway это касса FreeKassa.
type Gateway struct {
	cfg     Config
	baseURL string
	client  *http.Client
	nets    []*net.IPNet
}

// New собирает кассу. Пустой baseURL означает боевой API, nil вместо клиента означает клиент с таймаутом.
func New(cfg Config, baseURL string, client *http.Client) *Gateway {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	return &Gateway{cfg: cfg, baseURL: strings.TrimRight(baseURL, "/"), client: client}
}

// RestrictIPs включает фильтр: уведомления принимаются только с этих адресов.
// В RemoteAddr должен быть настоящий адрес (guard.RealIP). Без вызова фильтр выключен, как в тестах.
func (g *Gateway) RestrictIPs(nets []*net.IPNet) { g.nets = nets }

// OfficialNets это адреса, с которых FreeKassa шлёт уведомления (список из документации).
func OfficialNets() []*net.IPNet {
	var out []*net.IPNet
	for _, s := range []string{"168.119.157.136/32", "168.119.60.227/32", "178.154.197.79/32", "51.250.54.238/32"} {
		_, n, _ := net.ParseCIDR(s)
		out = append(out, n)
	}
	return out
}

func (g *Gateway) Name() string { return "freekassa" }

func (g *Gateway) Configured() bool {
	return g.cfg.Secret("freekassa.shop_id") != "" && g.cfg.Secret("freekassa.secret1") != "" &&
		g.cfg.Secret("freekassa.secret2") != ""
}

func md5hex(s string) string {
	sum := md5.Sum([]byte(s)) //nolint:gosec // см. импорт
	return hex.EncodeToString(sum[:])
}

func (g *Gateway) Create(_ context.Context, p core.Payment) (core.Invoice, error) {
	shop, s1 := g.cfg.Secret("freekassa.shop_id"), g.cfg.Secret("freekassa.secret1")
	if shop == "" || s1 == "" {
		return core.Invoice{}, errors.New("FreeKassa не настроена")
	}
	order, amount := strconv.FormatInt(p.ID, 10), formatMoney(p.Amount)
	q := url.Values{"m": {shop}, "oa": {amount}, "currency": {"RUB"}, "o": {order},
		"s": {md5hex(shop + ":" + amount + ":" + s1 + ":RUB:" + order)}, "lang": {"ru"}}
	return core.Invoice{ExternalID: order, URL: PayURL + "?" + q.Encode(), ExtAmount: p.Amount, ExtCurrency: "RUB"}, nil
}

// Verify проверяет уведомление: IP, затем подпись за постоянное время. Деньги и номер берём только из подписанной формы.
func (g *Gateway) Verify(r *http.Request) (core.GatewayEvent, error) {
	if len(g.nets) > 0 && !g.allowed(r.RemoteAddr) {
		return core.GatewayEvent{}, pay.ErrBadSignature
	}
	r.Body = http.MaxBytesReader(nil, r.Body, 1<<16)
	if err := r.ParseForm(); err != nil {
		return core.GatewayEvent{}, pay.ErrBadSignature
	}
	shop, s2 := g.cfg.Secret("freekassa.shop_id"), g.cfg.Secret("freekassa.secret2")
	merchant, amount, order := r.PostForm.Get("MERCHANT_ID"), r.PostForm.Get("AMOUNT"), r.PostForm.Get("MERCHANT_ORDER_ID")
	if shop == "" || s2 == "" || merchant != shop {
		return core.GatewayEvent{}, pay.ErrBadSignature
	}
	want := md5hex(merchant + ":" + amount + ":" + s2 + ":" + order)
	got := strings.ToLower(strings.TrimSpace(r.PostForm.Get("SIGN")))
	if !hmac.Equal([]byte(got), []byte(want)) {
		return core.GatewayEvent{}, pay.ErrBadSignature
	}
	cents, err := parseMoney(amount)
	id, err2 := strconv.ParseInt(order, 10, 64)
	if err != nil || err2 != nil || id <= 0 {
		return core.GatewayEvent{}, pay.ErrBadSignature
	}
	// Касса шлёт уведомление только об оплаченном заказе.
	return core.GatewayEvent{PaymentID: id, ExternalID: order, ExtAmount: cents, Status: core.PayPaid}, nil
}

func (g *Gateway) allowed(remote string) bool {
	host, _, err := net.SplitHostPort(remote)
	if err != nil {
		host = remote
	}
	ip := net.ParseIP(host)
	for _, n := range g.nets {
		if ip != nil && n.Contains(ip) {
			return true
		}
	}
	return false
}

type apiOrder struct {
	OrderID  string      `json:"merchant_order_id"`
	Amount   json.Number `json:"amount"`
	Currency string      `json:"currency"`
	Status   int         `json:"status"`
}

// Status спрашивает заказ у API (POST /orders). Без API-ключа сверка недоступна.
func (g *Gateway) Status(ctx context.Context, p core.Payment) (core.GatewayEvent, error) {
	shop, key := g.cfg.Secret("freekassa.shop_id"), g.cfg.Secret("freekassa.api_key")
	if key == "" {
		return core.GatewayEvent{}, errors.New("API-ключ FreeKassa не задан")
	}
	shopID, err := strconv.ParseInt(shop, 10, 64)
	if p.ExternalID == "" || err != nil {
		return core.GatewayEvent{}, errors.New("нет номера магазина или платежа для запроса к FreeKassa")
	}
	params := map[string]any{"shopId": shopID, "nonce": time.Now().UnixNano(), "paymentId": p.ExternalID}
	params["signature"] = apiSign(params, key)
	body, _ := json.Marshal(params)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.baseURL+"/orders", bytes.NewReader(body))
	if err != nil {
		return core.GatewayEvent{}, errors.New("не собран запрос к FreeKassa")
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := g.client.Do(req)
	if err != nil {
		return core.GatewayEvent{}, errors.New("FreeKassa недоступна") // текст ошибки сети не отдаём
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode/100 != 2 {
		return core.GatewayEvent{}, fmt.Errorf("FreeKassa ответила кодом %d", resp.StatusCode)
	}
	return eventFromAPI(raw, p.ExternalID)
}

// eventFromAPI разбирает ответ API и находит в нём наш заказ.
func eventFromAPI(raw []byte, order string) (core.GatewayEvent, error) {
	var out struct {
		Type   string     `json:"type"`
		Orders []apiOrder `json:"orders"`
	}
	if err := json.Unmarshal(raw, &out); err != nil || out.Type != "success" {
		return core.GatewayEvent{}, errors.New("ответ FreeKassa не разобран")
	}
	for _, o := range out.Orders {
		if o.OrderID != order {
			continue
		}
		cents, err := parseMoney(o.Amount.String())
		if err != nil || o.Currency != "RUB" {
			return core.GatewayEvent{}, errors.New("сумма или валюта заказа FreeKassa не разобраны")
		}
		id, _ := strconv.ParseInt(order, 10, 64)
		ev := core.GatewayEvent{PaymentID: id, ExternalID: order, ExtAmount: cents, Status: core.PayPending}
		switch o.Status {
		case 1:
			ev.Status = core.PayPaid
		case 6:
			ev.Status = core.PayRefunded
		case 8, 9:
			ev.Status = core.PayFailed
		}
		return ev, nil
	}
	return core.GatewayEvent{}, errors.New("заказ не найден в FreeKassa")
}

// apiSign это HMAC-SHA256 от значений параметров, взятых по порядку имён и склеенных через "|".
func apiSign(params map[string]any, key string) string {
	names := make([]string, 0, len(params))
	for k := range params {
		if k != "signature" {
			names = append(names, k)
		}
	}
	sort.Strings(names)
	vals := make([]string, len(names))
	for i, k := range names {
		vals[i] = fmt.Sprint(params[k])
	}
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write([]byte(strings.Join(vals, "|")))
	return hex.EncodeToString(mac.Sum(nil))
}

// formatMoney переводит копейки в сумму для формы: целые рубли без дроби ("300"), иначе "300.50".
func formatMoney(cents int64) string {
	if cents%100 == 0 {
		return strconv.FormatInt(cents/100, 10)
	}
	return fmt.Sprintf("%d.%02d", cents/100, cents%100)
}

// parseMoney разбирает рубли ("300", "300.5", "300.50") в копейки без float.
func parseMoney(s string) (int64, error) {
	whole, frac, _ := strings.Cut(strings.TrimSpace(s), ".")
	if len(frac) > 2 {
		if strings.Trim(frac[2:], "0") != "" {
			return 0, errors.New("сумма с долями копейки")
		}
		frac = frac[:2]
	}
	frac += strings.Repeat("0", 2-len(frac))
	w, err1 := strconv.ParseInt(whole, 10, 64)
	f, err2 := strconv.ParseInt(frac, 10, 64)
	if err1 != nil || err2 != nil || w < 0 || f < 0 {
		return 0, errors.New("сумма не разобрана")
	}
	return w*100 + f, nil
}
