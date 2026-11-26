// Package tribute это касса Tribute: оплата донатом через Telegram.
//
// API создания счёта на произвольную сумму у Tribute нет (в источниках его нет). Create отдаёт ссылку на донат
// из настроек, абонент вводит сумму сам. Вебхук new_donation несёт telegram_user_id и amount, но не наш номер
// платежа, поэтому платёж ищется среди ожидающих по Telegram ID и точной сумме (см. Pending).
package tribute

import (
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

	"github.com/bini69-oi/worker/core"
	"github.com/bini69-oi/worker/pay"
	"github.com/bini69-oi/worker/settings"
)

const (
	keyAPI  = "tribute.api_key"
	keyLink = "tribute.link"
)

func init() {
	settings.AddGateway("tribute", "Tribute",
		settings.Def{Key: keyAPI, Kind: settings.KindString, Secret: true, Group: "pay.tribute",
			Desc: "API-ключ Tribute: им подписываются вебхуки (заголовок trbt-signature)"},
		settings.Def{Key: keyLink, Kind: settings.KindString, Group: "pay.tribute",
			Desc: "Ссылка на донат Tribute, которую получает абонент. Вебхук укажите в Tribute: <адрес бота>/pay/tribute"},
	)
}

// Config это настройки, которые касса читает на лету.
type Config interface {
	String(key string) string
	Secret(key string) string
}

// PendingFunc находит платёж, которому принадлежит донат. Её подставляет сборка. Условия:
//   - платёж этой кассы (tribute) абонента с Telegram ID tgID и ожидаемой суммой amount в копейках;
//   - если платёж с номером доната key уже есть (повтор вебхука), вернуть его: ядро ответит «уже зачислено»;
//   - иначе самый старый ожидающий платёж;
//   - ничего нет: core.ErrNotFound.
type PendingFunc func(ctx context.Context, tgID, amount int64, key string) (core.Payment, error)

// Gateway это касса Tribute.
type Gateway struct {
	cfg Config
	// Pending ищет платёж по донату. Пока он nil, касса не настроена и абоненту не видна:
	// без него вебхук не к чему привязать.
	Pending PendingFunc
}

var _ pay.Gateway = (*Gateway)(nil)

// New собирает кассу.
func New(cfg Config) *Gateway { return &Gateway{cfg: cfg} }

func (g *Gateway) Name() string { return "tribute" }

func (g *Gateway) Configured() bool {
	return g.cfg.Secret(keyAPI) != "" && g.cfg.String(keyLink) != "" && g.Pending != nil
}

// Create отдаёт ссылку на донат. Номера платежа в кассе нет: его заполнит вебхук (ключ доната).
func (g *Gateway) Create(_ context.Context, p core.Payment) (core.Invoice, error) {
	link := g.cfg.String(keyLink)
	if link == "" {
		return core.Invoice{}, errors.New("ссылка Tribute не задана")
	}
	return core.Invoice{URL: link, ExtAmount: p.Amount, ExtCurrency: "RUB"}, nil
}

// Status всегда «ожидает»: у Tribute в источниках нет запроса статуса, оплата приходит только вебхуком.
func (g *Gateway) Status(_ context.Context, _ core.Payment) (core.GatewayEvent, error) {
	return core.GatewayEvent{Status: core.PayPending}, nil
}

// hook это вебхук Tribute: имя события и данные доната.
type hook struct {
	Name      string `json:"name"`
	CreatedAt string `json:"created_at"`
	Payload   struct {
		RequestID json.RawMessage `json:"donation_request_id"`
		TgID      json.RawMessage `json:"telegram_user_id"`
		Amount    json.RawMessage `json:"amount"`
		Currency  string          `json:"currency"`
	} `json:"payload"`
}

// Verify проверяет подпись HMAC-SHA256 (hex) тела запроса ключом из настроек и разбирает донат.
// Событие приходит как оплата, только если платёж удалось привязать к ожидающему.
func (g *Gateway) Verify(r *http.Request) (core.GatewayEvent, error) {
	key := g.cfg.Secret(keyAPI)
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		return core.GatewayEvent{}, errors.New("тело вебхука Tribute не прочитано")
	}
	if key == "" || !validSignature(key, body, r.Header.Get("trbt-signature")) {
		return core.GatewayEvent{}, pay.ErrBadSignature
	}
	var h hook
	if err := json.Unmarshal(body, &h); err != nil {
		return core.GatewayEvent{}, errors.New("вебхук Tribute не разобран")
	}
	if h.Name != "new_donation" && h.Name != "recurrent_donation" {
		return core.GatewayEvent{Status: core.PayPending}, nil // отмена подписки и прочее: принято, зачислять нечего
	}
	return g.donation(r.Context(), h)
}

func validSignature(key string, body []byte, got string) bool {
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write(body)
	want := hex.EncodeToString(mac.Sum(nil))
	return subtle.ConstantTimeCompare([]byte(strings.ToLower(strings.TrimSpace(got))), []byte(want)) == 1
}

// donation превращает оплаченный донат в событие и привязывает его к платежу.
func (g *Gateway) donation(ctx context.Context, h hook) (core.GatewayEvent, error) {
	tg, err1 := intOf(h.Payload.TgID)
	amount, err2 := intOf(h.Payload.Amount)
	if err1 != nil || err2 != nil || tg <= 0 || amount <= 0 {
		return core.GatewayEvent{}, errors.New("в вебхуке Tribute нет telegram_user_id или amount")
	}
	if c := h.Payload.Currency; c != "" && !strings.EqualFold(c, "RUB") {
		return core.GatewayEvent{}, fmt.Errorf("донат Tribute не в рублях: %s", c)
	}
	// У доната нет собственного номера: ключ из ссылки, абонента, суммы и времени события
	// одинаков при повторе доставки и различен у разных донатов.
	req := strings.Trim(string(h.Payload.RequestID), `"`)
	if req == "" || req == "null" || h.CreatedAt == "" {
		return core.GatewayEvent{}, errors.New("в вебхуке Tribute нет donation_request_id или created_at")
	}
	ev := core.GatewayEvent{ExternalID: fmt.Sprintf("%s_%d_%d_%s", req, tg, amount, h.CreatedAt),
		ExtAmount: amount, Status: core.PayPaid}
	p, err := g.Pending(ctx, tg, amount, ev.ExternalID)
	switch {
	case err == nil:
		ev.PaymentID = p.ID
	case errors.Is(err, core.ErrNotFound):
		// Ожидающего платежа нет: ядро ответит «неизвестный платёж», вебхук принят.
	default:
		return ev, errors.New("платёж Tribute не найден из-за ошибки хранилища")
	}
	return ev, nil
}

// intOf разбирает целое из JSON-числа или строки без float.
func intOf(raw json.RawMessage) (int64, error) {
	return strconv.ParseInt(strings.Trim(string(raw), `"`), 10, 64)
}
