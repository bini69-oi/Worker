// Package kassaai это касса Kassa AI (api.fk.life, семейство FreeKassa): заказ создаёт API v1 с подписью HMAC-SHA256,
// ссылку на оплату отдаёт ответ.
//
// Уведомление: POST-форма MERCHANT_ID, AMOUNT, MERCHANT_ORDER_ID, intid, SIGN, подпись MD5(магазин:сумма:секрет2:номер),
// сумма подписывается ровно в той строке, что пришла. В ответ касса ждёт слово YES (pay.Acker).
package kassaai

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/md5" // MD5 требует сама касса, это не наш выбор
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/bini69-oi/worker/core"
	"github.com/bini69-oi/worker/pay"
	"github.com/bini69-oi/worker/settings"
)

// DefaultBaseURL это адрес API Kassa AI.
const DefaultBaseURL = "https://api.fk.life/v1"

const (
	keyShop   = "kassaai.shop_id"
	keyAPI    = "kassaai.api_key"
	keySecret = "kassaai.secret2"
	keyMethod = "kassaai.method"
	keyIP     = "kassaai.server_ip"
	keyReturn = "kassaai.return_url"
)

func init() {
	settings.AddGateway("kassaai", "Карта и СБП (Kassa AI)",
		settings.Def{Key: keyShop, Kind: settings.KindString, Group: "pay.kassaai", Secret: true,
			Desc: "Номер магазина в Kassa AI"},
		settings.Def{Key: keyAPI, Kind: settings.KindString, Group: "pay.kassaai", Secret: true,
			Desc: "API-ключ магазина (подпись запросов)"},
		settings.Def{Key: keySecret, Kind: settings.KindString, Group: "pay.kassaai", Secret: true,
			Desc: "Секретное слово 2 (подпись уведомлений)"},
		settings.Def{Key: keyMethod, Kind: settings.KindString, Group: "pay.kassaai",
			Desc: "Номер способа оплаты: 44 СБП, 36 карты РФ, 43 SberPay. Пусто: 44"},
		settings.Def{Key: keyIP, Kind: settings.KindString, Group: "pay.kassaai",
			Desc: "Внешний IP сервера Worker: касса требует IP плательщика, а в боте его нет"},
		settings.Def{Key: keyReturn, Kind: settings.KindString, Group: "pay.kassaai",
			Desc: "Куда вернуть абонента после оплаты, {payment} заменится номером платежа. Пусто: обратно в бота"},
	)
}

// Config это настройки, которые касса читает на лету.
type Config interface {
	String(key string) string
	Secret(key string) string
}

// Gateway это касса Kassa AI.
type Gateway struct {
	cfg     Config
	baseURL string
	client  *http.Client
	nets    []*net.IPNet
	// Return это адрес возврата по умолчанию, если настройка пустая. Ставит сборка: диплинк в бота.
	Return func(paymentID int64) string
	// Hook это полный адрес уведомлений (https://домен/pay/kassaai). Пусто: адрес из кабинета кассы.
	Hook string
}

var (
	_ pay.Gateway = (*Gateway)(nil)
	_ pay.Acker   = (*Gateway)(nil)
)

// New собирает кассу. Пустой baseURL означает боевой API, nil вместо клиента означает клиент с таймаутом.
func New(cfg Config, baseURL string, client *http.Client) *Gateway {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	return &Gateway{cfg: cfg, baseURL: strings.TrimRight(baseURL, "/"), client: client}
}

// RestrictIPs включает фильтр: уведомления принимаются только с этих адресов. Без вызова фильтр выключен.
// В RemoteAddr должен быть настоящий адрес (guard.RealIP).
func (g *Gateway) RestrictIPs(nets []*net.IPNet) { g.nets = nets }

func (g *Gateway) Name() string { return "kassaai" }

func (g *Gateway) Configured() bool {
	return g.cfg.Secret(keyShop) != "" && g.cfg.Secret(keyAPI) != "" && g.cfg.Secret(keySecret) != ""
}

// Ack это ответ на принятое уведомление: касса ждёт слово YES.
func (g *Gateway) Ack(core.GatewayEvent) string { return "YES" }

func md5hex(s string) string {
	sum := md5.Sum([]byte(s))
	return hex.EncodeToString(sum[:])
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

// call подписывает запрос, шлёт его в API и отдаёт тело ответа. Ключ в ошибки не попадает.
func (g *Gateway) call(ctx context.Context, path string, params map[string]any) ([]byte, error) {
	params["shopId"] = g.shopID()
	params["nonce"] = time.Now().UnixNano()
	params["signature"] = apiSign(params, g.cfg.Secret(keyAPI))
	body, err := json.Marshal(params)
	if err != nil {
		return nil, errors.New("не собран запрос к кассе Kassa AI")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return nil, errors.New("не собран запрос к кассе Kassa AI")
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := g.client.Do(req)
	if err != nil {
		return nil, errors.New("касса Kassa AI недоступна") // текст ошибки сети не отдаём
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("касса Kassa AI ответила кодом %d", resp.StatusCode)
	}
	return raw, nil
}

// shopID отдаёт номер магазина числом, как требует API. Не число: строкой, касса сама откажет.
func (g *Gateway) shopID() any {
	s := g.cfg.Secret(keyShop)
	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		return n
	}
	return s
}

func (g *Gateway) Create(ctx context.Context, p core.Payment) (core.Invoice, error) {
	if !g.Configured() {
		return core.Invoice{}, errors.New("касса Kassa AI не настроена")
	}
	method := int64(44)
	if s := g.cfg.String(keyMethod); s != "" {
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return core.Invoice{}, errors.New("номер способа оплаты Kassa AI не число")
		}
		method = n
	}
	order := strconv.FormatInt(p.ID, 10)
	params := map[string]any{
		"paymentId": order, "i": method, "currency": "RUB", "amount": json.Number(formatMoney(p.Amount)),
		"email": "user_" + order + "@telegram.org", "ip": g.cfg.String(keyIP),
	}
	if back := pay.ReturnURL(g.cfg.String(keyReturn), g.Return, p.ID); back != "" {
		params["success_url"], params["fail_url"] = back, back
	}
	if g.Hook != "" {
		params["notification_url"] = g.Hook
	}
	raw, err := g.call(ctx, "/orders/create", params)
	if err != nil {
		return core.Invoice{}, err
	}
	var out struct {
		Type     string `json:"type"`
		Location string `json:"location"`
	}
	if json.Unmarshal(raw, &out) != nil || out.Type != "success" || out.Location == "" {
		return core.Invoice{}, errors.New("касса Kassa AI не создала заказ")
	}
	return core.Invoice{ExternalID: order, URL: out.Location, ExtAmount: p.Amount, ExtCurrency: "RUB"}, nil
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
	shop, s2 := g.cfg.Secret(keyShop), g.cfg.Secret(keySecret)
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

// Status спрашивает заказ у API (POST /orders) по нашему номеру платежа.
func (g *Gateway) Status(ctx context.Context, p core.Payment) (core.GatewayEvent, error) {
	if !g.Configured() || p.ExternalID == "" {
		return core.GatewayEvent{}, errors.New("нет ключей Kassa AI или номера платежа")
	}
	raw, err := g.call(ctx, "/orders", map[string]any{"paymentId": p.ExternalID})
	if err != nil {
		return core.GatewayEvent{}, err
	}
	var out struct {
		Type   string `json:"type"`
		Orders []struct {
			OrderID  string      `json:"merchant_order_id"`
			Amount   json.Number `json:"amount"`
			Currency string      `json:"currency"`
			Status   int         `json:"status"`
		} `json:"orders"`
	}
	if json.Unmarshal(raw, &out) != nil || out.Type != "success" {
		return core.GatewayEvent{}, errors.New("ответ Kassa AI не разобран")
	}
	for _, o := range out.Orders {
		if o.OrderID != p.ExternalID {
			continue
		}
		cents, err := parseMoney(o.Amount.String())
		if err != nil || o.Currency != "RUB" {
			return core.GatewayEvent{}, errors.New("сумма или валюта заказа Kassa AI не разобраны")
		}
		id, _ := strconv.ParseInt(p.ExternalID, 10, 64)
		ev := core.GatewayEvent{PaymentID: id, ExternalID: p.ExternalID, ExtAmount: cents, Status: core.PayPending}
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
	return core.GatewayEvent{}, errors.New("заказ не найден в Kassa AI")
}

// formatMoney переводит копейки в сумму без лишних нулей ("300", "300.5", "300.55"): так её считает подпись кассы.
func formatMoney(cents int64) string {
	s := fmt.Sprintf("%d.%02d", cents/100, cents%100)
	return strings.TrimSuffix(strings.TrimRight(s, "0"), ".")
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
