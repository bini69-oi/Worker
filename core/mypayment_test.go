package core_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/bini69-oi/worker/core"
)

func TestMyPayment(t *testing.T) {
	ctx := context.Background()
	e := newPayEnv(t)
	p, _, err := e.c.CreatePayment(ctx, e.user.ID, "fake", 30000)
	if err != nil {
		t.Fatal(err)
	}
	other, err := e.c.Register(ctx, 2002, "", "ru")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.c.MyPayment(ctx, other.ID, p.ID); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("чужой платёж: ждали ErrNotFound, получили %v", err)
	}

	got, err := e.c.MyPayment(ctx, e.user.ID, p.ID)
	if err != nil || got.Status != core.PayPending {
		t.Fatalf("не оплачен: %s %v", got.Status, err)
	}

	// Касса уже знает об оплате, а вебхук не пришёл: проверка зачисляет.
	e.fake.Paid = map[string]bool{p.ExternalID: true}
	if got, _ = e.c.MyPayment(ctx, e.user.ID, p.ID); got.Status != core.PayPending {
		t.Fatal("вторая сверка за 3 секунды дошла до кассы")
	}
	e.clock.Add(3 * time.Second)
	got, err = e.c.MyPayment(ctx, e.user.ID, p.ID)
	if err != nil || got.Status != core.PayPaid || e.balance(t) != 30000 {
		t.Fatalf("после сверки: %s, баланс %d (%v)", got.Status, e.balance(t), err)
	}
}

func TestFindPending(t *testing.T) {
	ctx := context.Background()
	e := newPayEnv(t)
	p1, _, _ := e.c.CreatePayment(ctx, e.user.ID, "fake", 30000)
	e.clock.Add(time.Second)
	if _, _, err := e.c.CreatePayment(ctx, e.user.ID, "fake", 30000); err != nil {
		t.Fatal(err)
	}
	e.clock.Add(time.Second)
	got, err := e.c.FindPending(ctx, "fake", 1001, 30000, "donation-1")
	if err != nil || got.ID != p1.ID {
		t.Fatalf("самый старый с той же суммой: %d %v, ждали %d", got.ID, err, p1.ID)
	}
	if _, err := e.c.FindPending(ctx, "fake", 1001, 12345, "donation-2"); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("другая сумма: %v", err)
	}
	if _, err := e.c.FindPending(ctx, "fake", 9999, 30000, "donation-3"); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("чужой абонент: %v", err)
	}
	// Повтор уведомления: платёж уже зачислен с этим номером, находим его же.
	if err := e.c.Credit(ctx, "fake", core.GatewayEvent{PaymentID: p1.ID, ExternalID: p1.ExternalID, ExtAmount: 30000, Status: core.PayPaid}); err != nil {
		t.Fatal(err)
	}
	if got, err = e.c.FindPending(ctx, "fake", 1001, 30000, p1.ExternalID); err != nil || got.ID != p1.ID {
		t.Fatalf("повтор по номеру кассы: %d %v", got.ID, err)
	}
}
