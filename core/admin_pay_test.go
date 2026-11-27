package core_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/bini69-oi/worker/core"
	"github.com/bini69-oi/worker/pay/manual"
)

// manualEnv включает ручную кассу и возвращает ядро с ней.
func manualEnv(t *testing.T) (*env, *core.Core) {
	t.Helper()
	e := newEnv(t)
	for k, v := range map[string]string{"manual.enabled": "true", "manual.details": "Карта 0000"} {
		if err := e.cfg.Set(k, v); err != nil {
			t.Fatal(err)
		}
	}
	c := core.New(core.Deps{Store: e.st, Clock: e.clock, Config: e.cfg, Panel: e.panel, Notifier: e.notes, OwnerID: owner,
		Gateways: []core.Gateway{manual.New(e.cfg)}})
	return e, c
}

func TestPaymentsFilters(t *testing.T) {
	ctx := context.Background()
	e, c := manualEnv(t)
	mk := func(amount int64) core.Payment {
		p, _, err := c.CreatePayment(ctx, e.user.ID, "manual", amount)
		if err != nil {
			t.Fatal(err)
		}
		e.clock.Add(time.Hour)
		return p
	}
	a, b, d := mk(10000), mk(20000), mk(30000)
	if err := c.ConfirmManual(ctx, owner, b.ID); err != nil {
		t.Fatal(err)
	}
	ids := func(p core.Page[core.PaymentRow]) []int64 {
		var out []int64
		for _, r := range p.Items {
			out = append(out, r.Payment.ID)
		}
		return out
	}
	cases := []struct {
		name string
		f    core.PaymentFilter
		want []int64
	}{
		{"все, новые сверху", core.PaymentFilter{}, []int64{d.ID, b.ID, a.ID}},
		{"ждущие", core.PaymentFilter{Status: core.PayPending}, []int64{d.ID, a.ID}},
		{"оплаченные", core.PaymentFilter{Status: core.PayPaid}, []int64{b.ID}},
		{"ждущие ручные", core.PaymentFilter{Status: core.PayPending, Gateway: "manual"}, []int64{d.ID, a.ID}},
		{"чужая касса", core.PaymentFilter{Gateway: "stars"}, nil},
		{"с даты", core.PaymentFilter{From: a.CreatedAt.Add(90 * time.Minute)}, []int64{d.ID}},
		{"до даты", core.PaymentFilter{To: b.CreatedAt}, []int64{a.ID}},
	}
	for _, cs := range cases {
		t.Run(cs.name, func(t *testing.T) {
			p, err := c.Payments(ctx, owner, cs.f, 0)
			if err != nil {
				t.Fatal(err)
			}
			got := ids(p)
			if len(got) != len(cs.want) {
				t.Fatalf("%v, ждали %v", got, cs.want)
			}
			for i := range got {
				if got[i] != cs.want[i] {
					t.Fatalf("%v, ждали %v", got, cs.want)
				}
			}
		})
	}
	p, _ := c.Payments(ctx, owner, core.PaymentFilter{}, d.ID)
	if len(p.Items) != 2 || p.Items[0].Payment.ID != b.ID {
		t.Errorf("курсор: %v", ids(p))
	}
	if p.Items[0].Username != "ivan" {
		t.Errorf("нет абонента в строке: %+v", p.Items[0])
	}
}

func TestPaymentsRights(t *testing.T) {
	ctx := context.Background()
	e, c := manualEnv(t)
	helperWith(t, e, 50, core.PermUsers)
	if _, err := c.Payments(ctx, 50, core.PaymentFilter{}, 0); !errors.Is(err, core.ErrForbidden) {
		t.Errorf("помощник без stats: %v", err)
	}
	if _, err := c.Payments(ctx, owner, core.PaymentFilter{Status: "чужой"}, 0); !errors.Is(err, core.ErrInvalid) {
		t.Errorf("неизвестный статус: %v", err)
	}
}

func TestConfirmPaymentTwice(t *testing.T) {
	ctx := context.Background()
	e, c := manualEnv(t)
	p, _, err := c.CreatePayment(ctx, e.user.ID, "manual", 10000)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.ConfirmPayment(ctx, owner, p.ID); err != nil {
		t.Fatal(err)
	}
	if err := c.ConfirmPayment(ctx, owner, p.ID); !errors.Is(err, core.ErrAlreadyPaid) {
		t.Fatalf("второй клик: %v", err)
	}
	if e.balance(t) != 10000 {
		t.Errorf("баланс %d: деньги зачислены не один раз", e.balance(t))
	}
}
