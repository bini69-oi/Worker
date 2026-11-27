package core_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/bini69-oi/worker/core"
	"github.com/bini69-oi/worker/pay/manual"
	"github.com/bini69-oi/worker/pay/stars"
)

// statsEnv это ядро с ручной кассой и Stars, чтобы были платежи двух касс и возврат.
func statsEnv(t *testing.T) (*cfgEnv, *core.Core, *stars.Gateway) {
	t.Helper()
	e := newCfgEnv(t)
	for k, v := range map[string]string{"manual.enabled": "true", "manual.details": "Карта 0000", "stars.enabled": "true"} {
		if err := e.cfg.Set(k, v); err != nil {
			t.Fatal(err)
		}
	}
	sg := stars.New(e.cfg)
	sg.Refunder = func(context.Context, int64, string) error { return nil }
	c := core.New(core.Deps{Store: e.st, Clock: e.clock, Config: e.cfg, Panel: e.panel, Notifier: e.notes, OwnerID: owner,
		Texts: e.tx, Gateways: []core.Gateway{manual.New(e.cfg), sg}})
	return e, c, sg
}

func janDay(d int) time.Time { return time.Date(2026, 1, d, 0, 0, 0, 0, time.UTC) }

func TestStatsToday(t *testing.T) {
	ctx := context.Background()
	_, c, _ := statsEnv(t)
	// t0 это 1 января 12:00. Трое пришли сегодня, один взял пробный, один оплатил.
	for tg := int64(2001); tg <= 2003; tg++ {
		if _, err := c.Register(ctx, tg, "u", "ru"); err != nil {
			t.Fatal(err)
		}
	}
	u, _ := c.UserByTg(ctx, 2001)
	if _, err := c.Trial(ctx, u.ID); err != nil {
		t.Fatal(err)
	}
	pay := func(userTg int64, amount int64) {
		usr, _ := c.UserByTg(ctx, userTg)
		p, _, err := c.CreatePayment(ctx, usr.ID, "manual", amount)
		if err != nil {
			t.Fatal(err)
		}
		if err := c.ConfirmManual(ctx, owner, p.ID); err != nil {
			t.Fatal(err)
		}
	}
	pay(2002, 100000)
	pay(2002, 50000)
	st, err := c.Stats(ctx, owner, janDay(1), janDay(2))
	if err != nil {
		t.Fatal(err)
	}
	// ivan из newEnv тоже пришёл сегодня.
	if st.NewUsers != 4 || st.Trials != 1 || st.FirstPaid != 1 || st.Revenue != 150000 || st.ActiveSubs != 1 {
		t.Errorf("сегодня: %+v", st)
	}
	if len(st.Days) != 1 || st.Days[0].Revenue != 150000 || len(st.Gateways) != 1 || st.Gateways[0] != (core.GatewayRevenue{Gateway: "manual", Revenue: 150000}) {
		t.Errorf("по дням и кассам: %+v", st)
	}
}

// За день оплачено 1000 ₽ и 200 ₽ возвращено: выручка дня 800 ₽.
func TestStatsRefundSameDay(t *testing.T) {
	ctx := context.Background()
	e, c, _ := statsEnv(t)
	u, _ := c.UserByTg(ctx, 1001)
	mk := func(gw string, amount int64) core.Payment {
		p, inv, err := c.CreatePayment(ctx, u.ID, gw, amount)
		if err != nil {
			t.Fatal(err)
		}
		ev := core.GatewayEvent{PaymentID: p.ID, ExternalID: "ext-" + gw + itoa(int(amount)), ExtAmount: inv.ExtAmount, Status: core.PayPaid}
		if err := c.Credit(ctx, gw, ev); err != nil {
			t.Fatal(err)
		}
		return p
	}
	mk("manual", 80000)
	back := mk("stars", 20000)
	e.clock.Add(time.Hour)
	if err := c.RefundStars(ctx, owner, back.ID); err != nil {
		t.Fatal(err)
	}
	st, err := c.Stats(ctx, owner, janDay(1), janDay(2))
	if err != nil {
		t.Fatal(err)
	}
	d := st.Days[0]
	if d.Gross != 100000 || d.Refunded != 20000 || d.Revenue != 80000 || st.Revenue != 80000 {
		t.Fatalf("день: %+v, всего %d", d, st.Revenue)
	}
	for _, g := range st.Gateways {
		if (g.Gateway == "manual" && g.Revenue != 80000) || (g.Gateway == "stars" && g.Revenue != 0) {
			t.Errorf("касса %s: %d", g.Gateway, g.Revenue)
		}
	}
}

// Возврат на следующий день вычитается из следующего дня, а не из дня оплаты.
func TestStatsRefundOnLaterDay(t *testing.T) {
	ctx := context.Background()
	e, c, _ := statsEnv(t)
	u, _ := c.UserByTg(ctx, 1001)
	p, inv, err := c.CreatePayment(ctx, u.ID, "stars", 30000)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Credit(ctx, "stars", core.GatewayEvent{PaymentID: p.ID, ExternalID: "late-1", ExtAmount: inv.ExtAmount, Status: core.PayPaid}); err != nil {
		t.Fatal(err)
	}
	e.clock.Add(24 * time.Hour) // уже 2 января
	if err := c.RefundStars(ctx, owner, p.ID); err != nil {
		t.Fatal(err)
	}
	st, err := c.Stats(ctx, owner, janDay(1), janDay(3))
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Days) != 2 || st.Days[0].Revenue != 30000 || st.Days[1].Revenue != -30000 || st.Revenue != 0 {
		t.Errorf("по дням: %+v, всего %d", st.Days, st.Revenue)
	}
}

func TestStatsPeriodLimit(t *testing.T) {
	ctx := context.Background()
	_, c, _ := statsEnv(t)
	if _, err := c.Stats(ctx, owner, janDay(1), janDay(1).AddDate(0, 0, 200)); !errors.Is(err, core.ErrPeriodTooLong) {
		t.Errorf("200 дней: %v", err)
	}
	if _, err := c.Stats(ctx, owner, janDay(1), janDay(1).AddDate(0, 0, 90)); err != nil {
		t.Errorf("ровно 90 дней: %v", err)
	}
	if _, err := c.Stats(ctx, owner, janDay(2), janDay(1)); !errors.Is(err, core.ErrInvalid) {
		t.Errorf("конец раньше начала: %v", err)
	}
}

func TestStatsStuckTasks(t *testing.T) {
	ctx := context.Background()
	e, c, _ := statsEnv(t)
	e.bought(t)
	e.panel.SetDown(true)
	if _, err := e.c.GrantDays(ctx, owner, e.user.ID, 5); err != nil {
		t.Fatal(err)
	}
	st, _ := c.Stats(ctx, owner, janDay(1), janDay(2))
	if st.StuckTasks != 0 {
		t.Fatalf("свежая задача не зависла: %d", st.StuckTasks)
	}
	e.clock.Add(2 * time.Hour)
	st, err := c.Stats(ctx, owner, janDay(1), janDay(2))
	if err != nil || st.StuckTasks != 1 || len(st.StuckUsers) != 1 || st.StuckUsers[0] != e.user.ID {
		t.Errorf("панель лежит два часа: %+v %v", st, err)
	}
}

func TestStatsRights(t *testing.T) {
	ctx := context.Background()
	e, c, _ := statsEnv(t)
	helperWith(t, e.env, 50, core.PermUsers)
	if _, err := c.Stats(ctx, 50, janDay(1), janDay(2)); !errors.Is(err, core.ErrForbidden) {
		t.Errorf("помощник без stats: %v", err)
	}
	helperWith(t, e.env, 51, core.PermStats)
	if _, err := c.Stats(ctx, 51, janDay(1), janDay(2)); err != nil {
		t.Errorf("помощник со stats: %v", err)
	}
}
