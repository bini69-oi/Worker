package core_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/bini69-oi/worker/core"
)

func TestPlanChangeNextPeriod(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	month := e.tariff(t, "Месяц", core.Period{Days: 30, Price: 100})
	year := e.tariff(t, "Год", core.Period{Days: 365, Price: 1000})
	e.topup(t, 5000)
	cur, err := e.c.Buy(ctx, e.user.ID, month.ID, 30)
	if err != nil {
		t.Fatal(err)
	}
	e.process(t)
	syncs := e.syncTasks()

	sub, err := e.c.Buy(ctx, e.user.ID, year.ID, 365)
	if err != nil {
		t.Fatal(err)
	}
	if sub.TariffID != month.ID || !sub.ExpiresAt.Equal(cur.ExpiresAt) || sub.NextTariffID != year.ID || sub.NextPrice != 1000 {
		t.Fatalf("смена не записалась как ждущая: %+v", sub)
	}
	if e.balance(t) != 5000-100-1000 || e.syncTasks() != syncs {
		t.Fatalf("баланс %d, задач панели +%d: смена должна списать сразу и не трогать панель", e.balance(t), e.syncTasks()-syncs)
	}
	if _, err := e.c.Buy(ctx, e.user.ID, month.ID, 30); !errors.Is(err, core.ErrPlanQueued) {
		t.Fatalf("вторая покупка при ждущей смене: %v", err)
	}

	// День окончания: включается год от старой даты окончания, подписка не истекает.
	e.clock.Add(31 * day)
	if _, err := e.c.ExpireDue(ctx); err != nil {
		t.Fatal(err)
	}
	got, _ := e.c.Sub(ctx, e.user.ID)
	if got.TariffID != year.ID || got.Status != core.SubActive || !got.ExpiresAt.Equal(cur.ExpiresAt.Add(365*day)) || got.NextTariffID != 0 {
		t.Fatalf("смена не включилась: %+v", got)
	}
	if e.syncTasks() == syncs {
		t.Fatal("после смены панель не получила задачу")
	}
}

func TestPlanCancelRefunds(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	month := e.tariff(t, "Месяц", core.Period{Days: 30, Price: 100})
	year := e.tariff(t, "Год", core.Period{Days: 365, Price: 1000})
	e.topup(t, 5000)
	if _, err := e.c.Buy(ctx, e.user.ID, month.ID, 30); err != nil {
		t.Fatal(err)
	}
	if _, err := e.c.Buy(ctx, e.user.ID, year.ID, 365); err != nil {
		t.Fatal(err)
	}
	if err := e.c.CancelPlan(ctx, e.user.ID); err != nil {
		t.Fatal(err)
	}
	if e.balance(t) != 4900 {
		t.Fatalf("после отмены баланс %d, ждали 4900", e.balance(t))
	}
	if err := e.c.CancelPlan(ctx, e.user.ID); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("повторная отмена: %v", err)
	}
	var sum int64
	hist, _ := e.c.History(ctx, e.user.ID, 100)
	for _, h := range hist {
		sum += h.Amount
	}
	if sum != e.balance(t) {
		t.Fatalf("сумма истории %d не равна балансу %d", sum, e.balance(t))
	}
}

func TestPlanDroppedWhenTariffGone(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	month := e.tariff(t, "Месяц", core.Period{Days: 30, Price: 100})
	year := e.tariff(t, "Год", core.Period{Days: 365, Price: 1000})
	e.topup(t, 5000)
	if _, err := e.c.Buy(ctx, e.user.ID, month.ID, 30); err != nil {
		t.Fatal(err)
	}
	if _, err := e.c.Buy(ctx, e.user.ID, year.ID, 365); err != nil {
		t.Fatal(err)
	}
	e.st.DeleteTariff(year.ID)
	e.clock.Add(31 * day)
	if _, err := e.c.ExpireDue(ctx); err != nil {
		t.Fatal(err)
	}
	got, _ := e.c.Sub(ctx, e.user.ID)
	if got.Status != core.SubExpired || got.NextTariffID != 0 || e.balance(t) != 4900 {
		t.Fatalf("смена на удалённый тариф: %+v, баланс %d", got, e.balance(t))
	}
	found := false
	for _, n := range notifyTasks(t, e) {
		found = found || n.Key == "notify.plan_dropped"
	}
	if !found {
		t.Fatal("абонент не узнал об отмене смены")
	}
}

func TestAutoRenewSkipsQueued(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	month := e.tariff(t, "Месяц", core.Period{Days: 30, Price: 100})
	year := e.tariff(t, "Год", core.Period{Days: 365, Price: 1000})
	e.topup(t, 5000)
	if _, err := e.c.Buy(ctx, e.user.ID, month.ID, 30); err != nil {
		t.Fatal(err)
	}
	if err := e.c.SetAutoRenew(ctx, e.user.ID, true); err != nil {
		t.Fatal(err)
	}
	if _, err := e.c.Buy(ctx, e.user.ID, year.ID, 365); err != nil {
		t.Fatal(err)
	}
	e.clock.Add(29*day + 12*time.Hour)
	before := e.balance(t)
	if _, err := e.c.AutoRenewDue(ctx); err != nil {
		t.Fatal(err)
	}
	if e.balance(t) != before {
		t.Fatal("автопродление списало деньги при ждущей смене")
	}
}
