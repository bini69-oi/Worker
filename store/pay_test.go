package store

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/bini69-oi/worker/core"
	"github.com/bini69-oi/worker/core/coretest"
	"github.com/bini69-oi/worker/pay/gatewaytest"
)

func payCore(t *testing.T) (*core.Core, *CoreStore, int64, *gatewaytest.Fake) {
	t.Helper()
	s, uid := newCoreStore(t)
	cfg := coretest.NewConfig(t)
	if err := cfg.Set("fake.enabled", "true"); err != nil {
		t.Fatal(err)
	}
	fake := &gatewaytest.Fake{Secret: "s", Paid: map[string]bool{}}
	c := core.New(core.Deps{Store: s, Clock: coretest.NewClock(tNow), Config: cfg, Gateways: []core.Gateway{fake}, OwnerID: 1})
	return c, s, uid, fake
}

// TestCreditConcurrentOnce: вебхуки и сверка приходят одновременно, а деньги зачисляются один раз.
func TestCreditConcurrentOnce(t *testing.T) {
	c, s, uid, fake := payCore(t)
	ctx := context.Background()
	p, _, err := c.CreatePayment(ctx, uid, "fake", 30000)
	if err != nil {
		t.Fatal(err)
	}
	fake.Paid[p.ExternalID] = true
	ev := core.GatewayEvent{ExternalID: p.ExternalID, ExtAmount: 30000, Status: core.PayPaid}

	var wg sync.WaitGroup
	for i := range 20 {
		wg.Go(func() {
			if i%2 == 0 {
				_ = c.Credit(ctx, "fake", ev)
			} else {
				_, _ = c.ReconcilePayments(ctx)
			}
		})
	}
	wg.Wait()
	u, _ := s.User(ctx, uid)
	var rows int
	if err := s.db.R.QueryRow("SELECT COUNT(*) FROM ledger WHERE ref_type = 'payment'").Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if u.Balance != 30000 || rows != 1 {
		t.Fatalf("баланс %d и строк зачисления %d, ждали 30000 и 1", u.Balance, rows)
	}
}

func TestPaymentRoundTrip(t *testing.T) {
	c, s, uid, _ := payCore(t)
	ctx := context.Background()
	p, inv, err := c.CreatePayment(ctx, uid, "fake", 30000)
	if err != nil {
		t.Fatal(err)
	}
	tx(t, s, func(tx core.Tx) error {
		got, err := tx.PaymentByExt("fake", inv.ExternalID)
		if err == nil && (got.ID != p.ID || got.ExtAmount != 30000 || got.ExtCurrency != "RUB" || got.Status != core.PayPending) {
			t.Errorf("платёж прочитан не так: %+v", got)
		}
		return err
	})
	pending, err := s.PendingPayments(ctx, tNow.Add(time.Second))
	if err != nil || len(pending) != 1 {
		t.Fatalf("ждали один ожидающий платёж: %d (%v)", len(pending), err)
	}
}

func TestPaymentExternalUnique(t *testing.T) {
	_, s, uid, _ := payCore(t)
	var a, b int64
	tx(t, s, func(tx core.Tx) error {
		var err error
		if a, err = tx.CreatePayment(core.Payment{UserID: uid, Gateway: "stars", Amount: 100, Status: core.PayPending, CreatedAt: tNow}); err != nil {
			return err
		}
		b, err = tx.CreatePayment(core.Payment{UserID: uid, Gateway: "stars", Amount: 100, Status: core.PayPending, CreatedAt: tNow})
		return err
	})
	tx(t, s, func(tx core.Tx) error {
		ok, err := tx.MarkPaid(a, "charge-1", tNow)
		if !ok {
			t.Error("первая оплата не отмечена")
		}
		return err
	})
	err := s.Tx(context.Background(), func(tx core.Tx) error { _, err := tx.MarkPaid(b, "charge-1", tNow); return err })
	if !errors.Is(err, core.ErrDuplicate) {
		t.Fatalf("тот же номер оплаты Telegram на другом платеже: ждали ErrDuplicate, получили %v", err)
	}
}
