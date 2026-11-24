package core_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/bini69-oi/worker/core"
	"github.com/bini69-oi/worker/pay/gatewaytest"
	"github.com/bini69-oi/worker/pay/manual"
)

type payEnv struct {
	*env
	fake *gatewaytest.Fake
}

func newPayEnv(t *testing.T) *payEnv {
	t.Helper()
	e := newEnv(t)
	fake := &gatewaytest.Fake{Secret: "s", Paid: map[string]bool{}}
	for k, v := range map[string]string{"fake.enabled": "true", "manual.enabled": "true", "manual.details": "Карта 0000"} {
		if err := e.cfg.Set(k, v); err != nil {
			t.Fatal(err)
		}
	}
	e.c = core.New(core.Deps{Store: e.st, Clock: e.clock, Config: e.cfg, Panel: e.panel, Notifier: e.notes,
		Gateways: []core.Gateway{fake, manual.New(e.cfg)}})
	return &payEnv{env: e, fake: fake}
}

func TestGatewaysVisibleAndOrdered(t *testing.T) {
	e := newPayEnv(t)
	if err := e.cfg.Set("manual.sort", "-1"); err != nil {
		t.Fatal(err)
	}
	list := e.c.Gateways()
	if len(list) != 2 || list[0].Name != "manual" || list[0].Title != "Перевод по реквизитам" {
		t.Fatalf("кассы не в том порядке: %+v", list)
	}
	if err := e.cfg.Set("manual.details", ""); err != nil {
		t.Fatal(err)
	}
	if list := e.c.Gateways(); len(list) != 1 || list[0].Name != "fake" {
		t.Fatalf("касса без реквизитов видна: %+v", list)
	}
	if _, _, err := e.c.CreatePayment(context.Background(), e.user.ID, "manual", 1000); !errors.Is(err, core.ErrNoGateway) {
		t.Fatalf("платёж в скрытую кассу: ждали ErrNoGateway, получили %v", err)
	}
}

func TestPaymentLimits(t *testing.T) {
	e := newPayEnv(t)
	for k, v := range map[string]string{"fake.min": "10000", "fake.max": "500000"} {
		if err := e.cfg.Set(k, v); err != nil {
			t.Fatal(err)
		}
	}
	for _, amount := range []int64{9999, 500001, 0, -5} {
		_, _, err := e.c.CreatePayment(context.Background(), e.user.ID, "fake", amount)
		var lim *core.LimitError
		if !errors.As(err, &lim) || lim.Min != 10000 || lim.Max != 500000 {
			t.Errorf("сумма %d: ждали LimitError с пределами, получили %v", amount, err)
		}
	}
	if _, _, err := e.c.CreatePayment(context.Background(), e.user.ID, "fake", 10000); err != nil {
		t.Fatalf("сумма на нижнем пределе: %v", err)
	}
}

func TestCreditNotifies(t *testing.T) {
	e := newPayEnv(t)
	ctx := context.Background()
	p, _, err := e.c.CreatePayment(ctx, e.user.ID, "fake", 30000)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.c.Credit(ctx, "fake", core.GatewayEvent{ExternalID: p.ExternalID, ExtAmount: 30000, Status: core.PayPaid}); err != nil {
		t.Fatal(err)
	}
	e.process(t)
	if b := balance(t, e.st, e.user.ID); b != 30000 {
		t.Fatalf("баланс %d", b)
	}
	if len(e.notes.ToUsers) != 1 || e.notes.ToUsers[0].Key != "notify.topup" ||
		len(e.notes.ToAdmins) != 1 || e.notes.ToAdmins[0].Topic != core.TopicPayments {
		t.Fatalf("уведомления о пополнении: %+v %+v", e.notes.ToUsers, e.notes.ToAdmins)
	}
	if got := e.st.Payment(p.ID); got.Status != core.PayPaid || got.PaidAt.IsZero() {
		t.Fatalf("платёж не отмечен оплаченным: %+v", got)
	}
}

func TestLostWebhookReconciled(t *testing.T) {
	e := newPayEnv(t)
	ctx := context.Background()
	p, _, err := e.c.CreatePayment(ctx, e.user.ID, "fake", 30000)
	if err != nil {
		t.Fatal(err)
	}
	e.fake.Paid[p.ExternalID] = true
	e.clock.Add(2 * time.Minute)
	for range 2 {
		if _, err := e.c.ReconcilePayments(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if b := balance(t, e.st, e.user.ID); b != 30000 {
		t.Fatalf("сверка: баланс %d, ждали 30000 один раз", b)
	}
}

func TestPendingExpires(t *testing.T) {
	e := newPayEnv(t)
	ctx := context.Background()
	p, _, err := e.c.CreatePayment(ctx, e.user.ID, "fake", 30000)
	if err != nil {
		t.Fatal(err)
	}
	e.clock.Add(23 * time.Hour)
	if _, err := e.c.ReconcilePayments(ctx); err != nil {
		t.Fatal(err)
	}
	if e.st.Payment(p.ID).Status != core.PayPending {
		t.Fatal("платёж истёк раньше срока")
	}
	e.clock.Add(2 * time.Hour)
	if _, err := e.c.ReconcilePayments(ctx); err != nil {
		t.Fatal(err)
	}
	if got := e.st.Payment(p.ID).Status; got != core.PayExpired {
		t.Fatalf("через 25 часов ждали «истёк», получили %s", got)
	}
	if err := e.c.Credit(ctx, "fake", core.GatewayEvent{ExternalID: p.ExternalID, ExtAmount: 30000, Status: core.PayPaid}); err != nil {
		t.Fatal(err)
	}
	if b := balance(t, e.st, e.user.ID); b != 0 {
		t.Fatalf("истёкший платёж зачислился: %d", b)
	}
}

func TestCheckPayment(t *testing.T) {
	e := newPayEnv(t)
	ctx := context.Background()
	p, _, err := e.c.CreatePayment(ctx, e.user.ID, "fake", 30000)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.c.CheckPayment(ctx, "fake", p.ID, 30000); err != nil {
		t.Fatalf("верный платёж: %v", err)
	}
	if err := e.c.CheckPayment(ctx, "fake", p.ID, 29999); !errors.Is(err, core.ErrAmountMismatch) {
		t.Fatalf("другая сумма: ждали ErrAmountMismatch, получили %v", err)
	}
	if err := e.c.CheckPayment(ctx, "manual", p.ID, 30000); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("чужая касса: ждали ErrNotFound, получили %v", err)
	}
}

func TestConfirmManual(t *testing.T) {
	e := newPayEnv(t)
	ctx := context.Background()
	p, inv, err := e.c.CreatePayment(ctx, e.user.ID, "manual", 50000)
	if err != nil {
		t.Fatal(err)
	}
	if inv.Text != "Карта 0000" {
		t.Fatalf("абонент не увидел реквизиты: %+v", inv)
	}
	if err := e.c.ConfirmManual(ctx, 42, p.ID); err != nil {
		t.Fatal(err)
	}
	if err := e.c.ConfirmManual(ctx, 42, p.ID); err != nil {
		t.Fatal(err)
	}
	if b := balance(t, e.st, e.user.ID); b != 50000 {
		t.Fatalf("баланс %d, ждали 50000 один раз", b)
	}
	other, _, _ := e.c.CreatePayment(ctx, e.user.ID, "fake", 1000)
	if err := e.c.ConfirmManual(ctx, 42, other.ID); !errors.Is(err, core.ErrForbidden) {
		t.Fatalf("подтверждение платежа настоящей кассы: ждали ErrForbidden, получили %v", err)
	}
}

func TestGatewayDownMarksFailed(t *testing.T) {
	e := newPayEnv(t)
	broken := &brokenGateway{}
	if err := e.cfg.Set("fake.enabled", "true"); err != nil {
		t.Fatal(err)
	}
	c := core.New(core.Deps{Store: e.st, Clock: e.clock, Config: e.cfg, Gateways: []core.Gateway{broken}})
	p, _, err := c.CreatePayment(context.Background(), e.user.ID, "fake", 1000)
	if !errors.Is(err, core.ErrGatewayDown) {
		t.Fatalf("ждали ErrGatewayDown, получили %v", err)
	}
	if got := e.st.Payment(p.ID).Status; got != core.PayFailed {
		t.Fatalf("платёж после отказа кассы: %s", got)
	}
}

// brokenGateway это касса, которая всегда падает при создании счёта.
type brokenGateway struct{ gatewaytest.Fake }

func (b *brokenGateway) Configured() bool { return true }
func (b *brokenGateway) Create(context.Context, core.Payment) (core.Invoice, error) {
	return core.Invoice{}, errors.New("касса упала")
}
