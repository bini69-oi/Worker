package core_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/bini69-oi/worker/core"
)

const keyA = "key-aaaaaaaaaaaaaaaa"

func TestBuyOnceReplayDoesNotChargeTwice(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	tr := e.tariff(t, "Месяц", core.Period{Days: 30, Price: 100})
	e.topup(t, 1000)
	first, err := e.c.BuyOrRemember(ctx, e.user.ID, tr.ID, 30, keyA)
	if err != nil || first.Replayed || first.Short != nil {
		t.Fatalf("первая покупка: %+v %v", first, err)
	}
	if e.balance(t) != 900 {
		t.Fatalf("баланс %d", e.balance(t))
	}
	again, err := e.c.BuyOrRemember(ctx, e.user.ID, tr.ID, 30, keyA)
	if err != nil || !again.Replayed || !again.Sub.ExpiresAt.Equal(first.Sub.ExpiresAt) {
		t.Fatalf("повтор: %+v %v", again, err)
	}
	if e.balance(t) != 900 {
		t.Fatalf("повтор списал снова: %d", e.balance(t))
	}
	// Другое действие, другой ключ: покупка идёт.
	if _, err := e.c.BuyOrRemember(ctx, e.user.ID, tr.ID, 30, "key-bbbbbbbbbbbbbbbb"); err != nil || e.balance(t) != 800 {
		t.Fatalf("новое действие: %v, баланс %d", err, e.balance(t))
	}
}

func TestBuyOnceConcurrentSameKey(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	tr := e.tariff(t, "Месяц", core.Period{Days: 30, Price: 100})
	e.topup(t, 1000)
	var wg sync.WaitGroup
	replays := make(chan bool, 8)
	for range 8 {
		wg.Go(func() {
			o, err := e.c.BuyOrRemember(ctx, e.user.ID, tr.ID, 30, keyA)
			if err != nil {
				t.Error(err)
			}
			replays <- o.Replayed
		})
	}
	wg.Wait()
	close(replays)
	fresh := 0
	for r := range replays {
		if !r {
			fresh++
		}
	}
	if fresh != 1 || e.balance(t) != 900 {
		t.Fatalf("новых покупок %d, баланс %d, ждали 1 и 900", fresh, e.balance(t))
	}
}

// Не хватило денег: выбор запомнен в той же транзакции, ключ не сгорел, после пополнения тот же ключ покупает.
func TestBuyOnceShortRemembersCartAndKeepsKey(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	tr := e.tariff(t, "Месяц", core.Period{Days: 30, Price: 100})
	e.topup(t, 40)
	o, err := e.c.BuyOrRemember(ctx, e.user.ID, tr.ID, 30, keyA)
	if err != nil || o.Short == nil || o.Short.Price != 100 || o.Short.Balance != 40 || o.Short.Missing != 60 {
		t.Fatalf("недостача: %+v %v", o, err)
	}
	if e.balance(t) != 40 {
		t.Fatal("деньги ушли")
	}
	e.topup(t, 100)
	o, err = e.c.BuyOrRemember(ctx, e.user.ID, tr.ID, 30, keyA)
	if err != nil || o.Replayed || o.Short != nil {
		t.Fatalf("тот же ключ после пополнения: %+v %v", o, err)
	}
}

func TestRequestKeyChecked(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	for _, k := range []string{"", "short", "has space 1234567890", "юникод-юникод-юникод", string(make([]byte, 65)), "key-aaaaaaaaaaaaaaaa!"} {
		if _, err := e.c.BuyOrRemember(ctx, e.user.ID, 1, 30, k); !errors.Is(err, core.ErrInvalid) {
			t.Errorf("ключ %q принят: %v", k, err)
		}
		if _, _, err := e.c.BuyTrafficOnce(ctx, e.user.ID, 10, k); !errors.Is(err, core.ErrInvalid) {
			t.Errorf("ключ %q принят докупкой: %v", k, err)
		}
	}
}

func TestAddonsAndCancelOnce(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	if err := e.cfg.Set("addons.traffic", "10:150"); err != nil {
		t.Fatal(err)
	}
	if err := e.cfg.Set("addons.device_month", "100"); err != nil {
		t.Fatal(err)
	}
	tr, err := e.c.SaveTariff(ctx, owner, core.Tariff{Name: "Лимит", Active: true, Devices: 3, TrafficBytes: 100 << 30,
		Periods: []core.Period{{Days: 30, Price: 100}}, Squads: []string{"a"}})
	if err != nil {
		t.Fatal(err)
	}
	e.topup(t, 2000)
	if _, err := e.c.Buy(ctx, e.user.ID, tr.ID, 30); err != nil {
		t.Fatal(err)
	}
	e.process(t)
	b0 := e.balance(t)
	for range 3 {
		if _, _, err := e.c.BuyTrafficOnce(ctx, e.user.ID, 10, keyA); err != nil {
			t.Fatal(err)
		}
		if _, _, err := e.c.BuyDeviceOnce(ctx, e.user.ID, "dev-aaaaaaaaaaaaaaaa"); err != nil {
			t.Fatal(err)
		}
	}
	sub, _ := e.c.Sub(ctx, e.user.ID)
	if sub.ExtraDevices != 1 || sub.ExtraTrafficBytes != 10<<30 {
		t.Fatalf("тройной повтор докупил: %+v", sub)
	}
	if e.balance(t) != b0-150-100 {
		t.Fatalf("списано %d, ждали 250", b0-e.balance(t))
	}
	// Один ключ у разных видов докупки не пересекается, у разных абонентов тоже.
	other := newEnvUser(t, e, 9009)
	if _, err := e.c.AdminAdjust(ctx, owner, other, 1000, "x"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.c.Buy(ctx, other, tr.ID, 30); err != nil {
		t.Fatal(err)
	}
	if _, replayed, err := e.c.BuyTrafficOnce(ctx, other, 10, keyA); err != nil || replayed {
		t.Fatalf("чужой ключ сработал у другого абонента: %v %v", replayed, err)
	}

	// Недостача при докупке несёт цену и баланс.
	if _, err := e.c.AdminAdjust(ctx, owner, other, -e.balanceOf(t, other)+50, "x"); err != nil {
		t.Fatal(err)
	}
	_, _, err = e.c.BuyTrafficOnce(ctx, other, 10, "key-cccccccccccccccc")
	var need *core.NeedMoney
	if !errors.As(err, &need) || need.Price != 150 || need.Balance != 50 || need.Missing != 100 {
		t.Fatalf("недостача докупки: %+v %v", need, err)
	}
}

func (e *env) balanceOf(t *testing.T, id int64) int64 {
	t.Helper()
	u, err := e.st.User(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return u.Balance
}

// Повтор после потерянного ответа, когда баланс уже потрачен: это повтор, а не «не хватает денег» с новой корзиной.
func TestBuyOnceReplayWithEmptyBalanceKeepsNoCart(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	tr := e.tariff(t, "Месяц", core.Period{Days: 30, Price: 100})
	e.topup(t, 100)
	if _, err := e.c.BuyOrRemember(ctx, e.user.ID, tr.ID, 30, keyA); err != nil {
		t.Fatal(err)
	}
	again, err := e.c.BuyOrRemember(ctx, e.user.ID, tr.ID, 30, keyA)
	if err != nil || !again.Replayed || again.Short != nil {
		t.Fatalf("повтор при нулевом балансе: %+v %v", again, err)
	}
	// Корзины нет: пополнение ничего само не купит.
	err = e.st.Tx(ctx, func(tx core.Tx) error {
		_, err := tx.PendingBuy(e.user.ID)
		return err
	})
	if !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("корзина запомнена при повторе: %v", err)
	}
}
