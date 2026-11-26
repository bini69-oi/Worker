// Package cryptobot это касса CryptoBot (Crypto Pay API, @CryptoBot в Telegram).
// Счёт выставляется в рублях (фиатный счёт), а оплатить его можно любой из разрешённых монет.
package cryptobot

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

// DefaultBaseURL это адрес Crypto Pay API. Для проверки на тестовой сети: https://testnet-pay.crypt.bot/api.
const DefaultBaseURL = "https://pay.crypt.bot/api"

const (
	keyToken  = "cryptobot.token"
	keyAssets = "cryptobot.assets"
	maxBody   = 1 << 20
)

func init() {
	settings.AddGateway("cryptobot", "CryptoBot",
		settings.Def{Key: keyToken, Kind: settings.KindString, Group: "pay.cryptobot", Secret: true,
			Desc: "Токен приложения из @CryptoBot (Crypto Pay, Создать приложение)"},
		settings.Def{Key: keyAssets, Kind: settings.KindString, Default: "USDT,TON", Group: "pay.cryptobot",
			Desc: "Монеты для оплаты через запятую, например USDT,TON,BTC. Пусто: любые"},
	)
}

// Config это настройки, которые касса читает на лету. Их даёт *settings.Settings.
type Config interface {
	String(key string) string
	Secret(key string) string
}

// Gateway это касса CryptoBot.
type Gateway struct {
	cfg     Config
	baseURL string
	client  *http.Client
}

// New собирает кассу. Пустой baseURL значит DefaultBaseURL, nil вместо клиента значит клиент с таймаутом.
func New(cfg Config, baseURL string, client *http.Client) *Gateway {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	return &Gateway{cfg: cfg, baseURL: strings.TrimRight(baseURL, "/"), client: client}
}

func (g *Gateway) Name() string     { return "cryptobot" }
func (g *Gateway) Configured() bool { return g.cfg.Secret(keyToken) != "" }

// amount это сумма счёта. API шлёт её строкой ("300.00"), но число тоже принимаем,
// чтобы смена формата не ломала приём оплат.
type amount string

func (a *amount) UnmarshalJSON(b []byte) error {
	if len(b) > 0 && b[0] == '"' {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		*a = amount(s)
		return nil
	}
	var n json.Number
	if err := json.Unmarshal(b, &n); err != nil {
		return err
	}
	*a = amount(n.String())
	return nil
}

// invoice это счёт в ответе API и в вебхуке (нужные нам поля).
type invoice struct {
	ID         int64  `json:"invoice_id"`
	Status     string `json:"status"`
	Payload    string `json:"payload"`
	Amount     amount `json:"amount"`
	Fiat       string `json:"fiat"`
	BotURL     string `json:"bot_invoice_url"`
	MiniAppURL string `json:"mini_app_invoice_url"`
}

type apiReply struct {
	OK     bool            `json:"ok"`
	Result json.RawMessage `json:"result"`
	Error  struct {
		Code int    `json:"code"`
		Name string `json:"name"`
	} `json:"error"`
}

// call зовёт метод API. Токен уходит только в заголовок и в ошибки не попадает.
func (g *Gateway) call(ctx context.Context, method, name string, query url.Values, body any, out any) error {
	token := g.cfg.Secret(keyToken)
	if token == "" {
		return errors.New("cryptobot: токен не задан")
	}
	var rd io.Reader
	u := g.baseURL + "/" + name
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(raw)
	}
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rd)
	if err != nil {
		return fmt.Errorf("cryptobot: запрос %s не собран", name)
	}
	req.Header.Set("Crypto-Pay-API-Token", token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := g.client.Do(req)
	if err != nil {
		// Ошибку транспорта не отдаём как есть: в ней может быть адрес запроса.
		return fmt.Errorf("cryptobot: касса не отвечает на %s", name)
	}
	defer resp.Body.Close()
	var reply apiReply
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxBody)).Decode(&reply); err != nil {
		return fmt.Errorf("cryptobot: ответ на %s не разобран (код %d)", name, resp.StatusCode)
	}
	if !reply.OK {
		return fmt.Errorf("cryptobot: касса отказала в %s: %s (код %d)", name, reply.Error.Name, reply.Error.Code)
	}
	if err := json.Unmarshal(reply.Result, out); err != nil {
		return fmt.Errorf("cryptobot: ответ на %s не разобран", name)
	}
	return nil
}

// Create выставляет фиатный счёт в рублях. В payload кладём наш номер платежа.
func (g *Gateway) Create(ctx context.Context, p core.Payment) (core.Invoice, error) {
	req := map[string]any{
		"currency_type": "fiat",
		"fiat":          "RUB",
		"amount":        formatKopeks(p.Amount),
		"payload":       payloadPrefix + strconv.FormatInt(p.ID, 10),
	}
	if assets := cleanAssets(g.cfg.String(keyAssets)); assets != "" {
		req["accepted_assets"] = assets
	}
	var inv invoice
	if err := g.call(ctx, http.MethodPost, "createInvoice", nil, req, &inv); err != nil {
		return core.Invoice{}, err
	}
	link := inv.BotURL
	if link == "" {
		link = inv.MiniAppURL
	}
	if inv.ID == 0 || link == "" {
		return core.Invoice{}, errors.New("cryptobot: касса не вернула счёт или ссылку")
	}
	return core.Invoice{ExternalID: strconv.FormatInt(inv.ID, 10), URL: link, ExtAmount: p.Amount, ExtCurrency: "RUB"}, nil
}

// Status спрашивает счёт по номеру: paid оплачен, expired истёк, остальное ожидает.
func (g *Gateway) Status(ctx context.Context, p core.Payment) (core.GatewayEvent, error) {
	q := url.Values{"invoice_ids": {p.ExternalID}}
	var res struct {
		Items []invoice `json:"items"`
	}
	if err := g.call(ctx, http.MethodGet, "getInvoices", q, nil, &res); err != nil {
		return core.GatewayEvent{}, err
	}
	for _, inv := range res.Items {
		if strconv.FormatInt(inv.ID, 10) != p.ExternalID {
			continue
		}
		return toEvent(inv)
	}
	return core.GatewayEvent{}, errors.New("cryptobot: счёта с таким номером у кассы нет")
}

// payloadPrefix метит наши счета: по нему чужой счёт того же приложения не примется за наш.
const payloadPrefix = "worker:"

// toEvent переводит счёт в событие. Сумму берём только из счёта, а не из нашей записи, чтобы сверка её поймала.
// Оплаченный счёт обязан быть в рублях и с суммой: иначе сравнивать не с чем.
func toEvent(inv invoice) (core.GatewayEvent, error) {
	ev := core.GatewayEvent{ExternalID: strconv.FormatInt(inv.ID, 10), Status: core.PayPending}
	if n, ok := strings.CutPrefix(inv.Payload, payloadPrefix); ok {
		ev.PaymentID, _ = strconv.ParseInt(n, 10, 64)
	}
	switch inv.Status {
	case "paid":
		ev.Status = core.PayPaid
	case "expired":
		ev.Status = core.PayExpired
	}
	if ev.Status != core.PayPaid {
		return ev, nil
	}
	if inv.Fiat != "RUB" {
		return core.GatewayEvent{}, fmt.Errorf("cryptobot: счёт выставлен не в рублях (%q)", inv.Fiat)
	}
	if inv.Amount == "" {
		return core.GatewayEvent{}, errors.New("cryptobot: в счёте нет суммы")
	}
	k, err := pay.Kopecks(string(inv.Amount))
	if err != nil {
		return core.GatewayEvent{}, err
	}
	ev.ExtAmount = k
	return ev, nil
}

// Verify проверяет подпись вебхука: hex(HMAC-SHA256(SHA256(токен), тело)) в заголовке crypto-pay-api-signature.
func (g *Gateway) Verify(r *http.Request) (core.GatewayEvent, error) {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxBody))
	if err != nil {
		return core.GatewayEvent{}, errors.New("cryptobot: тело вебхука не прочитано")
	}
	token := g.cfg.Secret(keyToken)
	got, decErr := hex.DecodeString(r.Header.Get("crypto-pay-api-signature"))
	if token == "" || decErr != nil || !hmac.Equal(got, sign(token, body)) {
		return core.GatewayEvent{}, pay.ErrBadSignature
	}
	var upd struct {
		Type    string  `json:"update_type"`
		Payload invoice `json:"payload"`
	}
	if err := json.Unmarshal(body, &upd); err != nil {
		return core.GatewayEvent{}, errors.New("cryptobot: вебхук не разобран")
	}
	if upd.Type != "invoice_paid" {
		// Другие события нам не нужны: ответ 200 без зачисления.
		return core.GatewayEvent{ExternalID: strconv.FormatInt(upd.Payload.ID, 10), Status: core.PayPending}, nil
	}
	upd.Payload.Status = "paid"
	return toEvent(upd.Payload)
}

func sign(token string, body []byte) []byte {
	key := sha256.Sum256([]byte(token))
	m := hmac.New(sha256.New, key[:])
	m.Write(body)
	return m.Sum(nil)
}

// cleanAssets убирает пробелы вокруг монет в списке из настройки.
func cleanAssets(s string) string {
	var out []string
	for _, a := range strings.Split(s, ",") {
		if a = strings.ToUpper(strings.TrimSpace(a)); a != "" {
			out = append(out, a)
		}
	}
	return strings.Join(out, ",")
}

// formatKopeks пишет копейки рублями: 30000 в "300.00".
func formatKopeks(k int64) string {
	return fmt.Sprintf("%d.%02d", k/100, k%100)
}
