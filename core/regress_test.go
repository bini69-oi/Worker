package core_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/bini69-oi/worker/core"
)

// Приглашённый ставит смену тарифа и отменяет её: деньги вернулись целиком, а бонус пригласившему остался.
func TestCancelledPlanGivesNoReferralBonus(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	if err := e.cfg.Set("ref.each_percent", "10"); err != nil {
		t.Fatal(err)
	}
	month := e.tariff(t, "Месяц", core.Period{Days: 30, Price: 15000})
	year := e.tariff(t, "Год", core.Period{Days: 365, Price: 100000})
	friend, _ := e.c.RegisterRef(ctx, 2002, "petr", "ru", e.user.RefCode)
	e.pay(t, friend.ID, 115000)
	if _, err := e.c.Buy(ctx, friend.ID, month.ID, 30); err != nil {
		t.Fatal(err)
	}
	start := balance(t, e.st, e.user.ID)
	for range 5 {
		if _, err := e.c.Buy(ctx, friend.ID, year.ID, 365); err != nil {
			t.Fatal(err)
		}
		if err := e.c.CancelPlan(ctx, friend.ID); err != nil {
			t.Fatal(err)
		}
	}
	if fb := balance(t, e.st, friend.ID); fb != 100000 {
		t.Fatalf("у приглашённого баланс %d", fb)
	}
	if got := balance(t, e.st, e.user.ID) - start; got != 0 {
		t.Fatalf("пригласивший получил %d копеек за 5 отменённых смен", got)
	}
	// Смена, которая дожила до начала, приносит бонус один раз и в день смены.
	if _, err := e.c.Buy(ctx, friend.ID, year.ID, 365); err != nil {
		t.Fatal(err)
	}
	if got := balance(t, e.st, e.user.ID) - start; got != 0 {
		t.Fatalf("бонус за смену до её начала: %d", got)
	}
	e.clock.Add(31 * day)
	if _, err := e.c.ExpireDue(ctx); err != nil {
		t.Fatal(err)
	}
	if got := balance(t, e.st, e.user.ID) - start; got != 10000 {
		t.Fatalf("бонус за включившуюся смену: %d, ждали 10000", got)
	}
}

// Спека addons: после продления докупленный трафик 0.
func TestExtraTrafficBurnsAfterRenewal(t *testing.T) {
	ctx := context.Background()
	e, tr := addonEnv(t)
	if _, err := e.c.BuyTraffic(ctx, e.user.ID, 10); err != nil {
		t.Fatal(err)
	}
	sub, err := e.c.Buy(ctx, e.user.ID, tr.ID, 30)
	if err != nil {
		t.Fatal(err)
	}
	e.clock.Add(31 * day) // начался оплаченный второй период
	if _, err := e.c.ExpireDue(ctx); err != nil {
		t.Fatal(err)
	}
	e.process(t)
	got, _ := e.c.Sub(ctx, e.user.ID)
	if got.ExtraTrafficBytes != 0 {
		t.Fatalf("после продления докупленный трафик %d (до %v), лимит в панели навсегда +10 ГБ", got.ExtraTrafficBytes, sub.ExpiresAt)
	}
}

// Автоподписка Stars на «Месяц», абонент перешёл на «Год»: очередное списание Stars само ставит смену обратно на «Месяц».
func TestStarsChargeKeepsSwitchedTariff(t *testing.T) {
	e := newStarsEnv(t)
	ctx := context.Background()
	year := e.env.tariff(t, "Год", core.Period{Days: 365, Price: 100000})
	if _, err := e.c.ChargeRecurring(ctx, e.user.ID, e.tariff.ID, "charge-1", 100); err != nil {
		t.Fatal(err)
	}
	e.topup(t, 100000)
	if _, err := e.c.Buy(ctx, e.user.ID, year.ID, 365); err != nil {
		t.Fatal(err)
	}
	e.clock.Add(30*24*time.Hour + time.Minute)
	if _, err := e.c.ExpireDue(ctx); err != nil {
		t.Fatal(err)
	}
	if s, _ := e.c.Sub(ctx, e.user.ID); s.TariffID != year.ID {
		t.Fatalf("год не включился: %+v", s)
	}
	before := e.balance(t)
	_, err := e.c.ChargeRecurring(ctx, e.user.ID, e.tariff.ID, "charge-2", 100)
	if !errors.Is(err, core.ErrOtherTariff) {
		t.Fatalf("списание Stars за старый тариф: %v", err)
	}
	s, _ := e.c.Sub(ctx, e.user.ID)
	if s.NextTariffID != 0 || e.balance(t) <= before {
		t.Fatalf("смена назад или деньги списания не на балансе: %+v, баланс %d → %d", s, before, e.balance(t))
	}
}

// Напоминание «подписка кончается» при уже оплаченной ждущей смене.
func TestNoRemindWithQueuedPlan(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	month := e.tariff(t, "Месяц", core.Period{Days: 30, Price: 100})
	year := e.tariff(t, "Год", core.Period{Days: 365, Price: 1000})
	e.topup(t, 5000)
	if _, err := e.c.Buy(ctx, e.user.ID, month.ID, 30); err != nil {
		t.Fatal(err)
	}
	if _, err := e.c.Buy(ctx, e.user.ID, year.ID, 365); err != nil {
		t.Fatal(err)
	}
	e.clock.Add(29 * day)
	n, err := e.c.RemindDue(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("%d напоминаний продлить, хотя следующий период уже оплачен", n)
	}
}
