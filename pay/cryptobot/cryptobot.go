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

// invoice это счёт в ответе API и в вебхуке (нужные нам поля).
type invoice struct {
	ID         int64  `json:"invoice_id"`
	Status     string `json:"status"`
	Payload    string `json:"payload"`
	Amount     string `json:"amount"`
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
		"payload":       strconv.FormatInt(p.ID, 10),
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
		return toEvent(inv, p.ExtAmount)
	}
	return core.GatewayEvent{}, errors.New("cryptobot: счёта с таким номером у кассы нет")
}

// toEvent переводит счёт в событие. Сумму берём из счёта, а не из нашей записи, чтобы сверка её поймала.
func toEvent(inv invoice, fallback int64) (core.GatewayEvent, error) {
	ev := core.GatewayEvent{ExternalID: strconv.FormatInt(inv.ID, 10), ExtAmount: fallback, Status: core.PayPending}
	if id, err := strconv.ParseInt(inv.Payload, 10, 64); err == nil {
		ev.PaymentID = id
	}
	if inv.Amount != "" {
		if inv.Fiat != "" && inv.Fiat != "RUB" {
			return core.GatewayEvent{}, fmt.Errorf("cryptobot: счёт выставлен не в рублях (%s)", inv.Fiat)
		}
		k, err := parseKopeks(inv.Amount)
		if err != nil {
			return core.GatewayEvent{}, err
		}
		ev.ExtAmount = k
	}
	switch inv.Status {
	case "paid":
		ev.Status = core.PayPaid
	case "expired":
		ev.Status = core.PayExpired
	}
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
	return toEvent(upd.Payload, 0)
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

// parseKopeks переводит "300.00" в 30000 без дробных чисел. Больше двух знаков допустимо только нулями.
func parseKopeks(s string) (int64, error) {
	bad := fmt.Errorf("cryptobot: сумма %q не разобрана", s)
	whole, frac, _ := strings.Cut(strings.TrimSpace(s), ".")
	if whole == "" || len(whole) > 12 || !digits(whole) || !digits(frac) {
		return 0, bad
	}
	if len(frac) > 2 {
		if strings.Trim(frac[2:], "0") != "" {
			return 0, bad
		}
		frac = frac[:2]
	}
	frac += strings.Repeat("0", 2-len(frac))
	w, _ := strconv.ParseInt(whole, 10, 64)
	f, _ := strconv.ParseInt(frac, 10, 64)
	return w*100 + f, nil
}

func digits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
