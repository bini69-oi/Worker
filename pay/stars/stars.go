// Package stars это оплата звёздами Telegram (валюта XTR). Счёт выставляет бот, оплата тоже приходит через бота,
// поэтому вебхука у кассы нет: бот передаёт ядру проверку перед оплатой и саму оплату.
package stars

import (
	"context"
	"errors"
	"net/http"

	"github.com/bini69-oi/worker/core"
	"github.com/bini69-oi/worker/pay"
	"github.com/bini69-oi/worker/settings"
)

func init() {
	settings.AddGateway("stars", "Telegram Stars")
}

// Config это настройки, которые читает касса.
type Config interface {
	Int(key string) int64
}

// RefundFunc возвращает звёзды через Telegram (метод refundStarPayment). Её подставляет бот.
type RefundFunc func(ctx context.Context, userTgID int64, chargeID string) error

// Gateway это касса Stars.
type Gateway struct {
	cfg Config
	// Refunder делает возврат. Пока бот не подключён, он nil и возврат недоступен.
	Refunder RefundFunc
}

// New возвращает кассу Stars. Курс берётся из настройки stars.rate (копеек за звезду).
func New(cfg Config) *Gateway { return &Gateway{cfg: cfg} }

var _ pay.Gateway = (*Gateway)(nil)

func (g *Gateway) Name() string { return "stars" }

// Configured истинно, когда задан курс. Отдельных ключей у Stars нет: счёт выставляет сам бот.
func (g *Gateway) Configured() bool { return g.cfg.Int("stars.rate") > 0 }

// Create считает, сколько звёзд стоит пополнение. Дробная звезда округляется вверх, чтобы не продать дешевле.
func (g *Gateway) Create(_ context.Context, p core.Payment) (core.Invoice, error) {
	return core.Invoice{ExtAmount: Stars(p.Amount, g.cfg.Int("stars.rate")), ExtCurrency: "XTR"}, nil
}

// Stars переводит копейки в звёзды по курсу с округлением вверх.
func Stars(kopecks, rate int64) int64 {
	if rate <= 0 {
		return 0
	}
	return (kopecks + rate - 1) / rate
}

// Status у Stars всегда «ожидает»: у Telegram нет запроса статуса, оплата приходит только через бота.
func (g *Gateway) Status(_ context.Context, p core.Payment) (core.GatewayEvent, error) {
	return core.GatewayEvent{Status: core.PayPending}, nil
}

// ErrNoRefunder значит, что бот ещё не подключил возврат.
var ErrNoRefunder = errors.New("возврат звёзд не подключён")

// Refund возвращает оплату Stars абоненту.
func (g *Gateway) Refund(ctx context.Context, p core.Payment, userTgID int64) error {
	if g.Refunder == nil {
		return ErrNoRefunder
	}
	if p.ExternalID == "" {
		return errors.New("у оплаты нет номера списания Telegram")
	}
	return g.Refunder(ctx, userTgID, p.ExternalID)
}

func (g *Gateway) Verify(*http.Request) (core.GatewayEvent, error) {
	return core.GatewayEvent{}, pay.ErrNoWebhook
}
