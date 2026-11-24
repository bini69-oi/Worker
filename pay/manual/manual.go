// Package manual это ручное зачисление: абонент видит реквизиты, переводит деньги, админ подтверждает платёж.
package manual

import (
	"context"
	"net/http"

	"github.com/bini69-oi/worker/core"
	"github.com/bini69-oi/worker/pay"
	"github.com/bini69-oi/worker/settings"
)

func init() {
	settings.AddGateway("manual", "Перевод по реквизитам", settings.Def{
		Key: "manual.details", Kind: settings.KindString, Group: "pay.manual",
		Desc: "Реквизиты и инструкция для абонента: куда переводить и что написать в комментарии",
	})
}

// Config это настройки, которые читает касса.
type Config interface {
	String(key string) string
}

// Gateway это ручная касса.
type Gateway struct {
	cfg Config
}

// New возвращает ручную кассу.
func New(cfg Config) *Gateway { return &Gateway{cfg: cfg} }

var _ pay.Gateway = (*Gateway)(nil)

func (g *Gateway) Name() string { return "manual" }

// Configured истинно, когда реквизиты заполнены: без них абоненту некуда переводить.
func (g *Gateway) Configured() bool { return g.cfg.String("manual.details") != "" }

func (g *Gateway) Create(_ context.Context, p core.Payment) (core.Invoice, error) {
	return core.Invoice{ExtAmount: p.Amount, ExtCurrency: "RUB", Text: g.cfg.String("manual.details")}, nil
}

// Status у ручной кассы всегда «ожидает»: оплату подтверждает только админ.
func (g *Gateway) Status(_ context.Context, p core.Payment) (core.GatewayEvent, error) {
	return core.GatewayEvent{Status: core.PayPending}, nil
}

func (g *Gateway) Verify(*http.Request) (core.GatewayEvent, error) {
	return core.GatewayEvent{}, pay.ErrNoWebhook
}
