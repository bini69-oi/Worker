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
