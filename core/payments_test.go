package core_test

import (
	"context"
	"errors"
	"strconv"
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
	e.c = core.New(core.Deps{Store: e.st, Clock: e.clock, Config: e.cfg, Panel: e.panel, Notifier: e.notes, OwnerID: owner,
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
	// Абонент узнаёт об истёкшем счёте один раз, повторная сверка не шлёт второе.
	if _, err := e.c.ReconcilePayments(ctx); err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, task := range notifyTasks(t, e.env) {
		if task.Key == "notify.pay_expired" && task.Vars["payment_id"] == strconv.FormatInt(p.ID, 10) {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("уведомлений «счёт истёк» %d, ждали 1", n)
	}
}

func TestCheckPayment(t *testing.T) {
	e := newPayEnv(t)
	ctx := context.Background()
	p, _, err := e.c.CreatePayment(ctx, e.user.ID, "fake", 30000)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.c.CheckPayment(ctx, e.user.ID, "fake", p.ID, 30000); err != nil {
		t.Fatalf("верный платёж: %v", err)
	}
	if err := e.c.CheckPayment(ctx, e.user.ID, "fake", p.ID, 29999); !errors.Is(err, core.ErrAmountMismatch) {
		t.Fatalf("другая сумма: ждали ErrAmountMismatch, получили %v", err)
	}
	if err := e.c.CheckPayment(ctx, e.user.ID, "manual", p.ID, 30000); !errors.Is(err, core.ErrNotFound) {
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
	if err := e.c.ConfirmManual(ctx, owner, p.ID); err != nil {
		t.Fatal(err)
	}
	if err := e.c.ConfirmManual(ctx, owner, p.ID); err != nil {
		t.Fatal(err)
	}
	if b := balance(t, e.st, e.user.ID); b != 50000 {
		t.Fatalf("баланс %d, ждали 50000 один раз", b)
	}
	other, _, _ := e.c.CreatePayment(ctx, e.user.ID, "fake", 1000)
	if err := e.c.ConfirmManual(ctx, owner, other.ID); !errors.Is(err, core.ErrForbidden) {
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

func TestLatePaymentAfterExpiry(t *testing.T) {
	e := newPayEnv(t)
	ctx := context.Background()
	p, _, err := e.c.CreatePayment(ctx, e.user.ID, "fake", 30000)
	if err != nil {
		t.Fatal(err)
	}
	e.clock.Add(25 * time.Hour)
	if _, err := e.c.ReconcilePayments(ctx); err != nil {
		t.Fatal(err)
	}
	if e.st.Payment(p.ID).Status != core.PayExpired {
		t.Fatal("платёж не истёк")
	}
	// Абонент всё же оплатил по старой ссылке: деньги пришли, их надо зачислить.
	if err := e.c.Credit(ctx, "fake", core.GatewayEvent{ExternalID: p.ExternalID, ExtAmount: 30000, Status: core.PayPaid}); err != nil {
		t.Fatal(err)
	}
	if b := balance(t, e.st, e.user.ID); b != 30000 {
		t.Fatalf("поздняя оплата потерялась: баланс %d", b)
	}
}

func TestManualConfirmAfterExpiry(t *testing.T) {
	e := newPayEnv(t)
	ctx := context.Background()
	p, _, err := e.c.CreatePayment(ctx, e.user.ID, "manual", 50000)
	if err != nil {
		t.Fatal(err)
	}
	e.clock.Add(48 * time.Hour)
	if _, err := e.c.ReconcilePayments(ctx); err != nil {
		t.Fatal(err)
	}
	if err := e.c.ConfirmManual(ctx, owner, p.ID); err != nil {
		t.Fatal(err)
	}
	if b := balance(t, e.st, e.user.ID); b != 50000 {
		t.Fatalf("ручное подтверждение через сутки не зачислило: %d", b)
	}
}

func TestForeignExternalIDNotSwallowed(t *testing.T) {
	e := newPayEnv(t)
	ctx := context.Background()
	a, _, _ := e.c.CreatePayment(ctx, e.user.ID, "fake", 1000)
	b, _, _ := e.c.CreatePayment(ctx, e.user.ID, "fake", 1000)
	err := e.c.Credit(ctx, "fake", core.GatewayEvent{PaymentID: a.ID, ExternalID: b.ExternalID, ExtAmount: 1000, Status: core.PayPaid})
	if err == nil {
		t.Fatal("чужой номер кассы принят как «уже зачислено»")
	}
	if e.st.Payment(a.ID).Status != core.PayPending {
		t.Fatal("платёж с чужим номером изменился")
	}
}

func TestCreditRejectsOtherExternalID(t *testing.T) {
	e := newPayEnv(t)
	ctx := context.Background()
	p, _, err := e.c.CreatePayment(ctx, e.user.ID, "fake", 30000)
	if err != nil {
		t.Fatal(err)
	}
	// Наш номер платежа тот, а номер кассы от старой оплаты: так бывает после отката базы из бэкапа.
	ev := core.GatewayEvent{PaymentID: p.ID, ExternalID: "fake-old", ExtAmount: 30000, Status: core.PayPaid}
	if err := e.c.Credit(ctx, "fake", ev); !errors.Is(err, core.ErrInvalid) {
		t.Fatalf("чужой номер кассы: ждали ErrInvalid, получили %v", err)
	}
	if b := e.balance(t); b != 0 {
		t.Fatalf("зачислено по чужому номеру: %d", b)
	}
}

func TestAmountMismatchAlertsOnce(t *testing.T) {
	e := newPayEnv(t)
	ctx := context.Background()
	p, _, err := e.c.CreatePayment(ctx, e.user.ID, "fake", 30000)
	if err != nil {
		t.Fatal(err)
	}
	ev := core.GatewayEvent{ExternalID: p.ExternalID, ExtAmount: 100, Status: core.PayPaid}
	for range 3 {
		if err := e.c.Credit(ctx, "fake", ev); !errors.Is(err, core.ErrAmountMismatch) {
			t.Fatalf("ждали ErrAmountMismatch, получили %v", err)
		}
	}
	e.process(t)
	n := 0
	for _, a := range e.notes.ToAdmins {
		if a.Key == "admin.amount_mismatch" {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("тревог о сумме %d, ждали 1", n)
	}
	if got := countNotify(t, e.env, "notify.pay_review"); got != 1 {
		t.Fatalf("абоненту «платёж на проверке» %d раз, ждали 1", got)
	}
}

// countNotify считает уведомления абоненту с ключом key в очереди.
func countNotify(t *testing.T, e *env, key string) int {
	t.Helper()
	n := 0
	for _, task := range notifyTasks(t, e) {
		if task.Key == key {
			n++
		}
	}
	return n
}

func TestRefundReversesCredit(t *testing.T) {
	e := newPayEnv(t)
	ctx := context.Background()
	p, _, err := e.c.CreatePayment(ctx, e.user.ID, "fake", 30000)
	if err != nil {
		t.Fatal(err)
	}
	paid := core.GatewayEvent{ExternalID: p.ExternalID, ExtAmount: 30000, Status: core.PayPaid}
	if err := e.c.Credit(ctx, "fake", paid); err != nil {
		t.Fatal(err)
	}
	back := core.GatewayEvent{ExternalID: p.ExternalID, Status: core.PayRefunded}
	for range 2 {
		if err := e.c.Credit(ctx, "fake", back); err != nil {
			t.Fatal(err)
		}
	}
	if b := e.balance(t); b != 0 {
		t.Fatalf("после возврата баланс %d, ждали 0", b)
	}
	if got := countNotify(t, e.env, "notify.refunded"); got != 1 {
		t.Fatalf("абоненту «платёж возвращён» %d раз, ждали 1", got)
	}
	if err := e.c.Credit(ctx, "fake", paid); err != nil {
		t.Fatal(err)
	}
	if b := e.balance(t); b != 0 {
		t.Fatalf("повтор оплаты после возврата зачислил: %d", b)
	}
}

func TestPaymentsPerUserLimited(t *testing.T) {
	e := newPayEnv(t)
	ctx := context.Background()
	for i := range 5 {
		if _, _, err := e.c.CreatePayment(ctx, e.user.ID, "fake", 1000); err != nil {
			t.Fatalf("счёт %d: %v", i+1, err)
		}
	}
	if _, _, err := e.c.CreatePayment(ctx, e.user.ID, "fake", 1000); !errors.Is(err, core.ErrTooManyTries) {
		t.Fatalf("шестой счёт за 10 минут: %v", err)
	}
	e.clock.Add(11 * time.Minute)
	if _, _, err := e.c.CreatePayment(ctx, e.user.ID, "fake", 1000); err != nil {
		t.Fatalf("после окна счёт не создан: %v", err)
	}
}
