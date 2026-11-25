package core_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/bini69-oi/worker/core"
)

func TestBuyEnoughMoney(t *testing.T) {
	e := newEnv(t)
	tr := e.tariff(t, "Месяц", core.Period{Days: 30, Price: 15000})
	e.topup(t, 20000)
	sub, err := e.c.Buy(context.Background(), e.user.ID, tr.ID, 30)
	if err != nil {
		t.Fatal(err)
	}
	if b := balance(t, e.st, e.user.ID); b != 5000 {
		t.Fatalf("баланс %d, ждали 5000", b)
	}
	if sub.Status != core.SubActive || sub.Price != 15000 || !sub.ExpiresAt.Equal(t0.Add(30*24*time.Hour)) {
		t.Fatalf("подписка не та: %+v", sub)
	}
	tasks := e.st.Tasks()
	var task core.PanelSyncTask
	if err := json.Unmarshal(tasks[len(tasks)-1].Payload, &task); err != nil || task.SubID != sub.ID {
		t.Fatalf("нет задачи синхронизации с панелью: %v %+v", err, tasks)
	}
}

func TestBuyNotEnoughMoney(t *testing.T) {
	e := newEnv(t)
	tr := e.tariff(t, "Месяц", core.Period{Days: 30, Price: 15000})
	e.topup(t, 10000)
	_, err := e.c.Buy(context.Background(), e.user.ID, tr.ID, 30)
	var need *core.NeedMoney
	if !errors.As(err, &need) || need.Missing != 5000 || !errors.Is(err, core.ErrNotEnoughMoney) {
		t.Fatalf("ждали NeedMoney на 5000, получили %v", err)
	}
	if b := balance(t, e.st, e.user.ID); b != 10000 {
		t.Fatalf("баланс %d, ждали 10000", b)
	}
	if _, err := e.c.Sub(context.Background(), e.user.ID); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("подписка появилась без денег: %v", err)
	}
}

func TestBuyExtendsLive(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	tr := e.tariff(t, "Месяц", core.Period{Days: 30, Price: 100})
	e.topup(t, 1000)
	first, _ := e.c.Buy(ctx, e.user.ID, tr.ID, 30)
	first.ExpiresAt = t0.Add(5 * 24 * time.Hour)
	e.st.SetSub(first)

	sub, err := e.c.Buy(ctx, e.user.ID, tr.ID, 30)
	if err != nil {
		t.Fatal(err)
	}
	if sub.ID != first.ID || !sub.ExpiresAt.Equal(t0.Add(35*24*time.Hour)) {
		t.Fatalf("ждали ту же подписку на 35 дней: %+v", sub)
	}
}

func TestBuyAfterExpiry(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	tr := e.tariff(t, "Месяц", core.Period{Days: 30, Price: 100})
	e.topup(t, 1000)
	first, _ := e.c.Buy(ctx, e.user.ID, tr.ID, 30)
	first.ExpiresAt, first.Status = t0.Add(-3*24*time.Hour), core.SubExpired
	e.st.SetSub(first)

	sub, err := e.c.Buy(ctx, e.user.ID, tr.ID, 30)
	if err != nil {
		t.Fatal(err)
	}
	if !sub.ExpiresAt.Equal(t0.Add(30*24*time.Hour)) || sub.Status != core.SubActive {
		t.Fatalf("ждали 30 дней от сегодня: %+v", sub)
	}
}

func TestPriceFrozen(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	tr := e.tariff(t, "Месяц", core.Period{Days: 30, Price: 15000})
	e.topup(t, 15000)
	if _, err := e.c.Buy(ctx, e.user.ID, tr.ID, 30); err != nil {
		t.Fatal(err)
	}
	tr.Periods = []core.Period{{Days: 30, Price: 20000}}
	if _, err := e.c.SaveTariff(ctx, owner, tr); err != nil {
		t.Fatal(err)
	}
	sub, _ := e.c.Sub(ctx, e.user.ID)
	hist, _ := e.c.History(ctx, e.user.ID, 1)
	if sub.Price != 15000 || hist[0].Amount != -15000 {
		t.Fatalf("цена не заморожена: подписка %d, история %d", sub.Price, hist[0].Amount)
	}
}

func TestHiddenTariff(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	tr := e.tariff(t, "Старый", core.Period{Days: 30, Price: 100})
	e.topup(t, 1000)
	if _, err := e.c.Buy(ctx, e.user.ID, tr.ID, 30); err != nil {
		t.Fatal(err)
	}
	tr.Active = false
	if _, err := e.c.SaveTariff(ctx, owner, tr); err != nil {
		t.Fatal(err)
	}
	if _, err := e.c.Buy(ctx, e.user.ID, tr.ID, 30); err != nil {
		t.Fatalf("свой скрытый тариф должен продлеваться: %v", err)
	}

	other, _ := e.c.Register(ctx, 2002, "petr", "ru")
	if _, err := e.c.AdminAdjust(ctx, 1, other.ID, 1000, "тест"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.c.Buy(ctx, other.ID, tr.ID, 30); !errors.Is(err, core.ErrTariffHidden) {
		t.Fatalf("новому абоненту скрытый тариф: ждали ErrTariffHidden, получили %v", err)
	}
}

func TestBuyOtherTariffWhileLive(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	a := e.tariff(t, "A", core.Period{Days: 30, Price: 100})
	b := e.tariff(t, "B", core.Period{Days: 30, Price: 100})
	e.topup(t, 1000)
	if _, err := e.c.Buy(ctx, e.user.ID, a.ID, 30); err != nil {
		t.Fatal(err)
	}
	if _, err := e.c.Buy(ctx, e.user.ID, b.ID, 30); !errors.Is(err, core.ErrOtherTariff) {
		t.Fatalf("ждали ErrOtherTariff, получили %v", err)
	}
	if b := balance(t, e.st, e.user.ID); b != 900 {
		t.Fatalf("деньги списались за отказ: %d", b)
	}
}

func TestBuyReplacesTrialKeepsDays(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	tr := e.tariff(t, "Месяц", core.Period{Days: 30, Price: 100})
	e.topup(t, 1000)
	trial, err := e.c.Trial(ctx, e.user.ID)
	if err != nil {
		t.Fatal(err)
	}
	sub, err := e.c.Buy(ctx, e.user.ID, tr.ID, 30)
	if err != nil {
		t.Fatal(err)
	}
	if sub.ID != trial.ID || sub.Trial || !sub.ExpiresAt.Equal(t0.Add(33*24*time.Hour)) {
		t.Fatalf("ждали платную подписку на 33 дня вместо пробной: %+v", sub)
	}
}

func TestBannedCannotBuy(t *testing.T) {
	e := newEnv(t)
	tr := e.tariff(t, "Месяц", core.Period{Days: 30, Price: 0})
	u := e.user
	u.BannedAt = t0
	e.st.SetUser(u)
	if _, err := e.c.Buy(context.Background(), u.ID, tr.ID, 30); !errors.Is(err, core.ErrBanned) {
		t.Fatalf("ждали ErrBanned, получили %v", err)
	}
}
