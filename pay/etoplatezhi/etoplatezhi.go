// Package etoplatezhi это касса «Это платежи» (платёжная страница paymentpage.etoplatezhi.ru).
//
// Счёт это ссылка на платёжную страницу с подписью HMAC-SHA512, API при создании не нужен. Вебхук подписан
// внутри тела (поле signature), но и после верной подписи статус и сумму касса переспрашивает у API (Gate API,
// POST /v2/payment/status): если API недоступен или не подтвердил оплату, вебхук отклоняется.
package etoplatezhi

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha512"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
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

const (
	// DefaultBaseURL это адрес Gate API, которым касса переспрашивает статус платежа.
	DefaultBaseURL = "https://gate.etoplatezhi.ru"
	// pageURL это платёжная страница.
	pageURL = "https://paymentpage.etoplatezhi.ru/payment"

	keyProject = "etoplatezhi.project_id"
	keySecret  = "etoplatezhi.secret_key"
	keyMethod  = "etoplatezhi.method"
	keyReturn  = "etoplatezhi.return_url"
	keyHook    = "etoplatezhi.hook_url"
	keyAPI     = "etoplatezhi.api_url"

	// orderPrefix метит наш номер платежа в payment_id, чтобы узнать его в вебхуке.
	orderPrefix = "worker_"
	maxBody     = 1 << 20
)

func init() {
	settings.AddGateway("etoplatezhi", "Карта и СБП (Это платежи)",
		settings.Def{Key: keyProject, Kind: settings.KindString, Secret: true, Group: "pay.etoplatezhi",
			Desc: "Номер проекта (project_id) из кабинета Это платежи"},
		settings.Def{Key: keySecret, Kind: settings.KindString, Secret: true, Group: "pay.etoplatezhi",
			Desc: "Секретный ключ проекта (подписывает счета, вебхуки и запросы к API)"},
		settings.Def{Key: keyMethod, Kind: settings.KindString, Group: "pay.etoplatezhi",
			Desc: "Способ оплаты: sbp или card. Пусто: абонент выбирает на странице"},
		settings.Def{Key: keyHook, Kind: settings.KindString, Group: "pay.etoplatezhi",
			Desc: "Адрес вебхука: https://ваш-сайт/pay/etoplatezhi. Пусто: подставит сборка"},
		settings.Def{Key: keyReturn, Kind: settings.KindString, Group: "pay.etoplatezhi",
			Desc: "Куда вернуть абонента после оплаты, {payment} заменится номером платежа. Пусто: обратно в бота"},
		settings.Def{Key: keyAPI, Kind: settings.KindString, Group: "pay.etoplatezhi",
			Desc: "Адрес Gate API, если касса дала другой. Пусто: адрес по умолчанию"},
	)
}

// Config это настройки, которые касса читает на лету.
type Config interface {
	String(key string) string
	Secret(key string) string
}

// Gateway это касса «Это платежи».
type Gateway struct {
	cfg     Config
	baseURL string
	client  *http.Client
	// Return это адрес возврата по умолчанию, если настройка пустая. Ставит сборка: диплинк в бота.
	Return func(paymentID int64) string
	// Hook это адрес вебхука по умолчанию, если настройка пустая. Ставит сборка: PUBLIC_URL/pay/etoplatezhi.
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

func (g *Gateway) Name() string { return "etoplatezhi" }

func (g *Gateway) Configured() bool {
	return g.cfg.Secret(keyProject) != "" && g.cfg.Secret(keySecret) != ""
}

// project отдаёт номер проекта: только цифры, иначе подпись не сойдётся с кассой.
func (g *Gateway) project() (json.Number, error) {
	id := strings.TrimSpace(g.cfg.Secret(keyProject))
	if n, err := strconv.ParseUint(id, 10, 63); err != nil || n == 0 {
		return "", errors.New("номер проекта Это платежи должен быть числом")
	}
	return json.Number(id), nil
}

// Create собирает подписанную ссылку на платёжную страницу. Сумма в копейках целым числом.
func (g *Gateway) Create(_ context.Context, p core.Payment) (core.Invoice, error) {
	project, err := g.project()
	if err != nil {
		return core.Invoice{}, err
	}
	if g.cfg.Secret(keySecret) == "" {
		return core.Invoice{}, errors.New("не задан секретный ключ Это платежи")
	}
	id := orderPrefix + strconv.FormatInt(p.ID, 10)
	params := map[string]any{
		"project_id":          project,
		"payment_id":          id,
		"payment_amount":      json.Number(strconv.FormatInt(p.Amount, 10)),
		"payment_currency":    "RUB",
		"customer_id":         strconv.FormatInt(p.UserID, 10),
		"payment_description": fmt.Sprintf("Пополнение %d", p.ID),
	}
	if m := strings.ToLower(strings.TrimSpace(g.cfg.String(keyMethod))); m == "sbp" || m == "card" {
		params["force_payment_method"] = m
	}
	if back := pay.ReturnURL(g.cfg.String(keyReturn), g.Return, p.ID); back != "" {
		params["redirect_success_url"], params["redirect_fail_url"] = back, back
	}
	hook := strings.TrimSpace(g.cfg.String(keyHook))
	if hook == "" {
		hook = g.Hook
	}
	if hook != "" {
		params["merchant_callback_url"] = hook
	}
	params["signature"] = sign(params, g.cfg.Secret(keySecret))
	q := url.Values{}
	for k, v := range params {
		q.Set(k, fmt.Sprint(v))
	}
	return core.Invoice{ExternalID: id, URL: pageURL + "?" + q.Encode(), ExtAmount: p.Amount, ExtCurrency: "RUB"}, nil
}

// payload это платёж в ответе API и в вебхуке.
type payload struct {
	Payment struct {
		ID     string `json:"id"`
		Status string `json:"status"`
		Sum    struct {
			Amount   json.Number `json:"amount"`
			Currency string      `json:"currency"`
		} `json:"sum"`
	} `json:"payment"`
}

// kopecks отдаёт сумму: целое число копеек в рублях, дробь, пустое значение и чужая валюта это ошибка.
func (p payload) kopecks() (int64, error) {
	n, err := strconv.ParseInt(string(p.Payment.Sum.Amount), 10, 64)
	if err != nil || n <= 0 {
		return 0, errors.New("в ответе Это платежи нет суммы в копейках")
	}
	if c := p.Payment.Sum.Currency; c != "" && !strings.EqualFold(c, "RUB") {
		return 0, errors.New("платёж Это платежи не в рублях")
	}
	return n, nil
}

func (g *Gateway) Status(ctx context.Context, p core.Payment) (core.GatewayEvent, error) {
	return g.confirm(ctx, p.ExternalID)
}

// confirm спрашивает у API статус платежа и сумму, если он оплачен.
func (g *Gateway) confirm(ctx context.Context, id string) (core.GatewayEvent, error) {
	project, err := g.project()
	if err != nil {
		return core.GatewayEvent{}, err
	}
	general := map[string]any{"project_id": project, "payment_id": id}
	general["signature"] = sign(map[string]any{"general": general}, g.cfg.Secret(keySecret))
	var out payload
	if err := g.post(ctx, "/v2/payment/status", map[string]any{"general": general}, &out); err != nil {
		return core.GatewayEvent{}, err
	}
	if out.Payment.Status == "" {
		return core.GatewayEvent{}, errors.New("касса Это платежи не вернула статус платежа")
	}
	ev := core.GatewayEvent{ExternalID: id, Status: mapStatus(out.Payment.Status)}
	if ev.Status == core.PayPaid {
		ev.ExtAmount, err = out.kopecks()
	}
	return ev, err
}

// Verify проверяет подпись вебхука (HMAC-SHA512 по всем полям тела), затем спрашивает статус и сумму у API.
// Подписанный вебхук принимается по подписи, сумма из него. API нужен только Status для сверки.
func (g *Gateway) Verify(r *http.Request) (core.GatewayEvent, error) {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxBody))
	if err != nil {
		return core.GatewayEvent{}, errors.New("вебхук кассы Это платежи не прочитан")
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	var raw map[string]any
	key := g.cfg.Secret(keySecret)
	if err := dec.Decode(&raw); err != nil || key == "" {
		return core.GatewayEvent{}, pay.ErrBadSignature
	}
	got, _ := raw["signature"].(string)
	if got == "" || !hmac.Equal([]byte(got), []byte(sign(raw, key))) {
		return core.GatewayEvent{}, pay.ErrBadSignature
	}
	var hook payload
	if err := json.Unmarshal(body, &hook); err != nil || hook.Payment.ID == "" {
		return core.GatewayEvent{}, errors.New("в вебхуке кассы Это платежи нет номера платежа")
	}
	ev := core.GatewayEvent{ExternalID: hook.Payment.ID, Status: mapStatus(hook.Payment.Status)}
	if n, ok := strings.CutPrefix(hook.Payment.ID, orderPrefix); ok {
		ev.PaymentID, _ = strconv.ParseInt(n, 10, 64)
	}
	if ev.Status != core.PayPaid && ev.Status != core.PayRefunded {
		return ev, nil
	}
	// Вебхук подписан ключом проекта, поэтому сумма берётся из него. Переспрос API здесь не нужен:
	// адрес и вид ответа статуса не сверены с живой кассой, и при расхождении ни одна оплата не зачислилась бы.
	// Status с API остаётся запасной сверкой для потерянных вебхуков.
	if ev.Status == core.PayPaid {
		n, err := hook.kopecks()
		if err != nil || n <= 0 {
			return core.GatewayEvent{}, errors.New("в вебхуке кассы Это платежи нет суммы")
		}
		ev.ExtAmount = n
	}
	return ev, nil
}

func mapStatus(s string) core.PayStatus {
	switch strings.ToLower(s) {
	case "success":
		return core.PayPaid
	case "decline", "error", "cancelled":
		return core.PayFailed
	case "refunded", "reversed":
		return core.PayRefunded
	}
	return core.PayPending // processing, awaiting ..., partially refunded
}

// sign считает подпись: поля раскладываются в строки «ключ:значение» (вложенность через двоеточие, булевы 1 и 0,
// пустые списки и null пропускаются, signature и frame_mode игнорируются), сортируются, склеиваются через «;»,
// затем HMAC-SHA512 с секретным ключом и base64.
func sign(params map[string]any, key string) string {
	var parts []string
	flatten(params, "", &parts)
	sort.Strings(parts)
	m := hmac.New(sha512.New, []byte(key))
	m.Write([]byte(strings.Join(parts, ";")))
	return base64.StdEncoding.EncodeToString(m.Sum(nil))
}

func flatten(v any, prefix string, out *[]string) {
	switch x := v.(type) {
	case map[string]any:
		for k, val := range x {
			if k == "signature" || k == "frame_mode" {
				continue
			}
			flatten(val, join(prefix, k), out)
		}
	case []any:
		for i, val := range x {
			flatten(val, join(prefix, strconv.Itoa(i)), out)
		}
	case bool:
		*out = append(*out, prefix+":"+map[bool]string{true: "1", false: "0"}[x])
	case nil:
	default:
		*out = append(*out, prefix+":"+fmt.Sprint(x))
	}
}

func join(prefix, k string) string {
	if prefix == "" {
		return k
	}
	return prefix + ":" + k
}

// post шлёт запрос в API. Ключ участвует только в подписи и в ошибки не попадает.
func (g *Gateway) post(ctx context.Context, path string, in, out any) error {
	b, err := json.Marshal(in)
	if err != nil {
		return err
	}
	base := g.baseURL
	if s := strings.TrimSpace(g.cfg.String(keyAPI)); s != "" {
		base = strings.TrimRight(s, "/")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+path, bytes.NewReader(b))
	if err != nil {
		return errors.New("не собран запрос к Это платежи")
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := g.client.Do(req)
	if err != nil {
		return errors.New("касса Это платежи недоступна")
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("касса Это платежи ответила кодом %d", resp.StatusCode)
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxBody)).Decode(out); err != nil {
		return errors.New("ответ кассы Это платежи не разобран")
	}
	return nil
}
