package core_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/bini69-oi/worker/core"
)

// pay пополняет баланс через кассу, как настоящий платёж.
func (e *payEnv) pay(t *testing.T, amount int64) {
	t.Helper()
	ctx := context.Background()
	p, _, err := e.c.CreatePayment(ctx, e.user.ID, "fake", amount)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.c.Credit(ctx, "fake", core.GatewayEvent{ExternalID: p.ExternalID, ExtAmount: amount, Status: core.PayPaid}); err != nil {
		t.Fatal(err)
	}
}

func notified(e *env, key string) int {
	n := 0
	for _, u := range e.notes.ToUsers {
		if u.Key == key {
			n++
		}
	}
	return n
}

func TestSmartCart(t *testing.T) {
	e := newPayEnv(t)
	ctx := context.Background()
	tr := e.tariff(t, "Месяц", core.Period{Days: 30, Price: 15000})
	e.pay(t, 5000)
	if err := e.c.RememberBuy(ctx, e.user.ID, tr.ID, 30); err != nil {
		t.Fatal(err)
	}
	e.pay(t, 10000)
	e.process(t)
	sub, err := e.c.Sub(ctx, e.user.ID)
	if err != nil || sub.Status != core.SubActive {
		t.Fatalf("после пополнения подписка не куплена: %+v (%v)", sub, err)
	}
	if b := balance(t, e.st, e.user.ID); b != 0 {
		t.Fatalf("баланс %d, ждали 0", b)
	}
	if notified(e.env, "notify.bought") != 1 {
		t.Fatalf("абонент не получил уведомление о покупке: %+v", e.notes.ToUsers)
	}
	e.pay(t, 15000)
	if b := balance(t, e.st, e.user.ID); b != 15000 {
		t.Fatalf("корзина сработала второй раз: баланс %d", b)
	}
}

func TestSmartCartWaitsForEnoughMoney(t *testing.T) {
	e := newPayEnv(t)
	ctx := context.Background()
	tr := e.tariff(t, "Месяц", core.Period{Days: 30, Price: 15000})
	if err := e.c.RememberBuy(ctx, e.user.ID, tr.ID, 30); err != nil {
		t.Fatal(err)
	}
	e.pay(t, 10000)
	if _, err := e.c.Sub(ctx, e.user.ID); err == nil {
		t.Fatal("подписка куплена без денег")
	}
	e.pay(t, 5000)
	if _, err := e.c.Sub(ctx, e.user.ID); err != nil {
		t.Fatalf("после второго пополнения подписка не куплена: %v", err)
	}
}

func TestSmartCartExpires(t *testing.T) {
	e := newPayEnv(t)
	ctx := context.Background()
	tr := e.tariff(t, "Месяц", core.Period{Days: 30, Price: 15000})
	if err := e.c.RememberBuy(ctx, e.user.ID, tr.ID, 30); err != nil {
		t.Fatal(err)
	}
	e.clock.Add(25 * time.Hour)
	e.pay(t, 15000)
	if _, err := e.c.Sub(ctx, e.user.ID); err == nil {
		t.Fatal("устаревший выбор купился сам")
	}
	if b := balance(t, e.st, e.user.ID); b != 15000 {
		t.Fatalf("деньги должны остаться на балансе, баланс %d", b)
	}
}

func TestAutoRenewEnoughMoney(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	sub := e.bought(t) // 30 дней за 100, на балансе 9900
	if err := e.c.SetAutoRenew(ctx, e.user.ID, true); err != nil {
		t.Fatal(err)
	}
	e.clock.Add(29 * 24 * time.Hour) // остался 1 день
	for range 2 {
		if _, err := e.c.AutoRenewDue(ctx); err != nil {
			t.Fatal(err)
		}
	}
	e.process(t)
	after, _ := e.c.Sub(ctx, e.user.ID)
	if !after.ExpiresAt.Equal(sub.ExpiresAt.Add(30*24*time.Hour)) || balance(t, e.st, e.user.ID) != 9800 {
		t.Fatalf("ждали одно продление на 30 дней за 100: срок %v, баланс %d", after.ExpiresAt, balance(t, e.st, e.user.ID))
	}
	if notified(e, "notify.autorenew_ok") != 1 {
		t.Fatalf("нет уведомления о продлении: %+v", e.notes.ToUsers)
	}
}

func TestAutoRenewNotEnoughMoney(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	tr := e.tariff(t, "Месяц", core.Period{Days: 30, Price: 15000})
	e.topup(t, 15000)
	if _, err := e.c.Buy(ctx, e.user.ID, tr.ID, 30); err != nil {
		t.Fatal(err)
	}
	if err := e.c.SetAutoRenew(ctx, e.user.ID, true); err != nil {
		t.Fatal(err)
	}
	e.topup(t, 5000)
	e.clock.Add(29 * 24 * time.Hour)
	for range 3 {
		if _, err := e.c.AutoRenewDue(ctx); err != nil {
			t.Fatal(err)
		}
	}
	e.process(t)
	if notified(e, "notify.autorenew_fail") != 1 {
		t.Fatalf("ждали одно уведомление о нехватке: %+v", e.notes.ToUsers)
	}
	for _, u := range e.notes.ToUsers {
		if u.Key == "notify.autorenew_fail" && u.Vars["missing"] != "10000" {
			t.Fatalf("не хватает 10000, а в уведомлении %q", u.Vars["missing"])
		}
	}
	if b := balance(t, e.st, e.user.ID); b != 5000 {
		t.Fatalf("деньги списались без продления: %d", b)
	}
}

func TestAutoRenewOffByDefault(t *testing.T) {
	e := newEnv(t)
	e.bought(t)
	e.clock.Add(29 * 24 * time.Hour)
	if n, _ := e.c.AutoRenewDue(context.Background()); n != 0 {
		t.Fatalf("продлено без включённого автопродления: %d", n)
	}
}

func TestAutoRenewRetriesAfterTopup(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	tr := e.tariff(t, "Месяц", core.Period{Days: 30, Price: 15000})
	e.topup(t, 15000)
	if _, err := e.c.Buy(ctx, e.user.ID, tr.ID, 30); err != nil {
		t.Fatal(err)
	}
	if err := e.c.SetAutoRenew(ctx, e.user.ID, true); err != nil {
		t.Fatal(err)
	}
	e.clock.Add(29 * 24 * time.Hour)
	if n, _ := e.c.AutoRenewDue(ctx); n != 0 {
		t.Fatal("продлено без денег")
	}
	e.topup(t, 15000)
	e.clock.Add(10 * time.Minute)
	if n, _ := e.c.AutoRenewDue(ctx); n != 1 {
		t.Fatal("после пополнения автопродление не повторилось")
	}
}

func TestAutoRenewWindowNotTwice(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	if err := e.cfg.Set("autorenew.days_before", "3"); err != nil {
		t.Fatal(err)
	}
	tr := e.tariff(t, "День", core.Period{Days: 1, Price: 100})
	e.topup(t, 10000)
	if _, err := e.c.Buy(ctx, e.user.ID, tr.ID, 1); err != nil {
		t.Fatal(err)
	}
	if err := e.c.SetAutoRenew(ctx, e.user.ID, true); err != nil {
		t.Fatal(err)
	}
	e.clock.Add(13 * time.Hour) // осталось 11 часов, окно половина дня
	for range 5 {
		if _, err := e.c.AutoRenewDue(ctx); err != nil {
			t.Fatal(err)
		}
		e.clock.Add(10 * time.Minute)
	}
	if b := balance(t, e.st, e.user.ID); b != 10000-100-100 {
		t.Fatalf("подписка на день продлилась несколько раз подряд: баланс %d", b)
	}
}

func TestAutoRenewOffOnPermanentError(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	tr := e.tariff(t, "Месяц", core.Period{Days: 30, Price: 100})
	e.topup(t, 1000)
	if _, err := e.c.Buy(ctx, e.user.ID, tr.ID, 30); err != nil {
		t.Fatal(err)
	}
	if err := e.c.SetAutoRenew(ctx, e.user.ID, true); err != nil {
		t.Fatal(err)
	}
	tr.Periods = []core.Period{{Days: 90, Price: 300}} // срок 30 дней убрали
	if _, err := e.c.SaveTariff(ctx, owner, tr); err != nil {
		t.Fatal(err)
	}
	e.clock.Add(29 * 24 * time.Hour)
	if _, err := e.c.AutoRenewDue(ctx); err != nil {
		t.Fatal(err)
	}
	sub, _ := e.c.Sub(ctx, e.user.ID)
	e.process(t)
	if sub.AutoRenew || notified(e, "notify.autorenew_off") != 1 {
		t.Fatalf("автопродление не выключено или абонент не знает: %+v %+v", sub, e.notes.ToUsers)
	}
}

func TestPromoSubCannotAutoRenewWithoutPeriod(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	tr := e.tariff(t, "Месяц", core.Period{Days: 30, Price: 100})
	e.promo(t, core.Promo{Code: "FREE7", Kind: core.PromoDays, Value: 7, TariffID: tr.ID})
	if _, err := e.c.ApplyPromo(ctx, e.user.ID, "FREE7"); err != nil {
		t.Fatal(err)
	}
	if err := e.c.SetAutoRenew(ctx, e.user.ID, true); !errors.Is(err, core.ErrNoPeriod) {
		t.Fatalf("автопродление на 7 дней без такого срока: ждали ErrNoPeriod, получили %v", err)
	}
}

func TestAutoRenewSkipsIfUserRenewedMeanwhile(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	tr := e.tariff(t, "Месяц", core.Period{Days: 30, Price: 100})
	e.topup(t, 1000)
	if _, err := e.c.Buy(ctx, e.user.ID, tr.ID, 30); err != nil {
		t.Fatal(err)
	}
	if err := e.c.SetAutoRenew(ctx, e.user.ID, true); err != nil {
		t.Fatal(err)
	}
	e.clock.Add(29 * 24 * time.Hour)
	if _, err := e.c.Buy(ctx, e.user.ID, tr.ID, 30); err != nil { // абонент продлил сам
		t.Fatal(err)
	}
	if n, _ := e.c.AutoRenewDue(ctx); n != 0 {
		t.Fatal("автопродление списало второй раз после ручного продления")
	}
	if b := balance(t, e.st, e.user.ID); b != 800 {
		t.Fatalf("баланс %d, ждали 800", b)
	}
}

func TestBanDoesNotTurnOffAutoRenew(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.bought(t)
	if err := e.c.SetAutoRenew(ctx, e.user.ID, true); err != nil {
		t.Fatal(err)
	}
	u := e.user
	u.BannedAt = t0
	e.st.SetUser(u)
	e.clock.Add(29 * 24 * time.Hour)
	if _, err := e.c.AutoRenewDue(ctx); err != nil {
		t.Fatal(err)
	}
	if sub, _ := e.c.Sub(ctx, e.user.ID); !sub.AutoRenew {
		t.Fatal("бан выключил автопродление навсегда")
	}
}
