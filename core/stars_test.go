package core_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/bini69-oi/worker/core"
	"github.com/bini69-oi/worker/pay/stars"
)

type starsEnv struct {
	*env
	gw     *stars.Gateway
	tariff core.Tariff
	refund error // что ответит Telegram на возврат
	asked  []string
}

func newStarsEnv(t *testing.T) *starsEnv {
	t.Helper()
	e := &starsEnv{env: newEnv(t)}
	if err := e.cfg.Set("stars.enabled", "true"); err != nil {
		t.Fatal(err)
	}
	e.gw = stars.New(e.cfg)
	e.gw.Refunder = func(_ context.Context, tg int64, charge string) error {
		e.asked = append(e.asked, charge)
		return e.refund
	}
	e.c = core.New(core.Deps{Store: e.st, Clock: e.clock, Config: e.cfg, Panel: e.panel, Notifier: e.notes,
		Gateways: []core.Gateway{e.gw}})
	e.tariff = e.env.tariff(t, "Месяц", core.Period{Days: 30, Price: 15000})
	return e
}

func TestRecurringCharge(t *testing.T) {
	e := newStarsEnv(t)
	ctx := context.Background()
	sub, err := e.c.ChargeRecurring(ctx, e.user.ID, e.tariff.ID, "charge-1", 100)
	if err != nil {
		t.Fatal(err)
	}
	if !sub.ExpiresAt.Equal(t0.Add(30 * 24 * time.Hour)) {
		t.Fatalf("первое списание: срок %v", sub.ExpiresAt)
	}
	if _, err := e.c.ChargeRecurring(ctx, e.user.ID, e.tariff.ID, "charge-1", 100); err != nil {
		t.Fatal(err)
	}
	again, _ := e.c.Sub(ctx, e.user.ID)
	if !again.ExpiresAt.Equal(sub.ExpiresAt) {
		t.Fatalf("повтор того же списания продлил подписку: %v", again.ExpiresAt)
	}
	e.clock.Add(30 * 24 * time.Hour)
	next, err := e.c.ChargeRecurring(ctx, e.user.ID, e.tariff.ID, "charge-2", 100)
	if err != nil {
		t.Fatal(err)
	}
	if !next.ExpiresAt.Equal(t0.Add(60 * 24 * time.Hour)) {
		t.Fatalf("второе списание: срок %v", next.ExpiresAt)
	}
	if b := balance(t, e.st, e.user.ID); b != 0 {
		t.Fatalf("деньги автоподписки должны уйти на продление, баланс %d", b)
	}
}

func TestRecurringCanceledLivesToEnd(t *testing.T) {
	e := newStarsEnv(t)
	ctx := context.Background()
	if _, err := e.c.ChargeRecurring(ctx, e.user.ID, e.tariff.ID, "charge-1", 100); err != nil {
		t.Fatal(err)
	}
	// Абонент отменил автоподписку в Telegram: следующего списания нет.
	e.clock.Add(29 * 24 * time.Hour)
	if n, _ := e.c.ExpireDue(ctx); n != 0 {
		t.Fatal("подписка отключилась до конца оплаченного срока")
	}
	e.clock.Add(24 * time.Hour)
	if n, _ := e.c.ExpireDue(ctx); n != 1 {
		t.Fatal("подписка без списания не закончилась в срок")
	}
}

func TestRefundStars(t *testing.T) {
	e := newStarsEnv(t)
	ctx := context.Background()
	p, inv, err := e.c.CreatePayment(ctx, e.user.ID, "stars", 30000)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.c.Credit(ctx, "stars", core.GatewayEvent{PaymentID: p.ID, ExternalID: "charge-9", ExtAmount: inv.ExtAmount, Status: core.PayPaid}); err != nil {
		t.Fatal(err)
	}
	if err := e.c.RefundStars(ctx, 1, p.ID); err != nil {
		t.Fatal(err)
	}
	if len(e.asked) != 1 || e.asked[0] != "charge-9" {
		t.Fatalf("Telegram не спросили о возврате: %v", e.asked)
	}
	if got := e.st.Payment(p.ID).Status; got != core.PayRefunded {
		t.Fatalf("платёж после возврата: %s", got)
	}
	if b := balance(t, e.st, e.user.ID); b != 0 {
		t.Fatalf("после возврата баланс %d, ждали 0", b)
	}
	if err := e.c.RefundStars(ctx, 1, p.ID); !errors.Is(err, core.ErrInvalid) {
		t.Fatalf("повторный возврат: ждали ErrInvalid, получили %v", err)
	}
}

func TestRefundCutsDaysWhenMoneySpent(t *testing.T) {
	e := newStarsEnv(t)
	ctx := context.Background()
	sub, err := e.c.ChargeRecurring(ctx, e.user.ID, e.tariff.ID, "charge-1", 100)
	if err != nil {
		t.Fatal(err)
	}
	var paid core.Payment
	for id := int64(1); id < 50; id++ {
		if p := e.st.Payment(id); p.Recurring {
			paid = p
		}
	}
	if err := e.c.RefundStars(ctx, 1, paid.ID); err != nil {
		t.Fatal(err)
	}
	after, _ := e.c.Sub(ctx, e.user.ID)
	if !after.ExpiresAt.Equal(sub.ExpiresAt.Add(-30 * 24 * time.Hour)) {
		t.Fatalf("деньги потрачены на подписку, ждали срез 30 дней: было %v, стало %v", sub.ExpiresAt, after.ExpiresAt)
	}
}

func TestRefundRejectedByTelegram(t *testing.T) {
	e := newStarsEnv(t)
	ctx := context.Background()
	e.refund = errors.New("CHARGE_ALREADY_REFUNDED")
	p, inv, err := e.c.CreatePayment(ctx, e.user.ID, "stars", 30000)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.c.Credit(ctx, "stars", core.GatewayEvent{PaymentID: p.ID, ExternalID: "charge-7", ExtAmount: inv.ExtAmount, Status: core.PayPaid}); err != nil {
		t.Fatal(err)
	}
	err = e.c.RefundStars(ctx, 1, p.ID)
	if !errors.Is(err, core.ErrRefundFailed) || !strings.Contains(err.Error(), "CHARGE_ALREADY_REFUNDED") {
		t.Fatalf("ждали отказ с причиной, получили %v", err)
	}
	if got := e.st.Payment(p.ID).Status; got != core.PayPaid {
		t.Fatalf("после отказа платёж изменился: %s", got)
	}
	if b := balance(t, e.st, e.user.ID); b != 30000 {
		t.Fatalf("после отказа баланс %d", b)
	}
}
