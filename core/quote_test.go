package core_test

import (
	"context"
	"errors"
	"testing"

	"github.com/bini69-oi/worker/core"
)

func TestQuote(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	month := e.tariff(t, "Месяц", core.Period{Days: 30, Price: 25000})
	other := e.tariff(t, "Другой", core.Period{Days: 30, Price: 100})

	e.topup(t, 10000)
	q, err := e.c.Quote(ctx, e.user.ID, month.ID, 30)
	if err != nil {
		t.Fatal(err)
	}
	if q != (core.Quote{Price: 25000, Balance: 10000, Missing: 15000}) {
		t.Fatalf("не хватает: %+v", q)
	}

	e.topup(t, 20000)
	if q, _ = e.c.Quote(ctx, e.user.ID, month.ID, 30); q.Missing != 0 {
		t.Fatalf("хватает: %+v", q)
	}
	if b := e.balance(t); b != 30000 {
		t.Fatalf("расчёт списал деньги: баланс %d", b)
	}

	if _, err := e.c.Buy(ctx, e.user.ID, month.ID, 30); err != nil {
		t.Fatal(err)
	}
	if _, err := e.c.Quote(ctx, e.user.ID, other.ID, 30); !errors.Is(err, core.ErrOtherTariff) {
		t.Fatalf("другой тариф: ждали ErrOtherTariff, получили %v", err)
	}

	other.Active = false
	if _, err := e.c.SaveTariff(ctx, owner, other); err != nil {
		t.Fatal(err)
	}
	fresh := newEnvUser(t, e, 2002)
	if _, err := e.c.Quote(ctx, fresh, other.ID, 30); !errors.Is(err, core.ErrTariffHidden) {
		t.Fatalf("скрытый тариф: ждали ErrTariffHidden, получили %v", err)
	}
}

// newEnvUser регистрирует ещё одного абонента в том же окружении.
func newEnvUser(t *testing.T, e *env, tgID int64) int64 {
	t.Helper()
	u, err := e.c.Register(context.Background(), tgID, "", "ru")
	if err != nil {
		t.Fatal(err)
	}
	return u.ID
}
