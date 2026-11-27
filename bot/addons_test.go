package bot

import (
	"context"
	"strings"
	"testing"

	"github.com/bini69-oi/worker/core"
)

func TestAddonScreens(t *testing.T) {
	e := newEnv(t)
	e.set(t, "addons.traffic", "10:15000,50:50000", "addons.device_month", "10000")
	u := e.start(t)
	tr := e.tariff(t, "Месяц", core.Period{Days: 30, Price: 25000})
	e.topup(t, u.ID, 100000)
	if _, err := e.c.Buy(context.Background(), u.ID, tr.ID, 30); err != nil {
		t.Fatal(err)
	}
	e.sync(t)
	e.press(t, userTg, 5, "sub")
	if got := render(e.api.take()); !strings.Contains(got, "→ at]") || !strings.Contains(got, "→ ad]") {
		t.Fatalf("на экране подписки нет докупки: %s", got)
	}
	e.press(t, userTg, 5, "at")
	golden(t, "addons_traffic", e.api.take())
	e.press(t, userTg, 5, "at:10")
	e.press(t, userTg, 5, "at:10") // двойной клик
	if got, _ := e.c.UserByTg(context.Background(), userTg); got.Balance != 100000-25000-15000 {
		t.Fatalf("двойной клик по пакету: баланс %d", got.Balance)
	}
	e.api.take()
	e.press(t, userTg, 5, "ad")
	golden(t, "addons_device", e.api.take())
	e.press(t, userTg, 5, "ad:ok")
	sub, _ := e.c.Sub(context.Background(), u.ID)
	if sub.ExtraDevices != 1 || sub.ExtraTrafficBytes != 10<<30 {
		t.Fatalf("докуплено: %+v", sub)
	}
}

func TestAddonNotEnoughMoney(t *testing.T) {
	e := newEnv(t)
	e.set(t, "addons.traffic", "10:15000")
	u := e.start(t)
	tr := e.tariff(t, "Месяц", core.Period{Days: 30, Price: 25000})
	e.topup(t, u.ID, 25000)
	if _, err := e.c.Buy(context.Background(), u.ID, tr.ID, 30); err != nil {
		t.Fatal(err)
	}
	e.press(t, userTg, 5, "at:10")
	if got := render(e.api.take()); !strings.Contains(got, "Не хватает <b>150 ₽</b>") || !strings.Contains(got, "tp:15000") {
		t.Fatalf("не хватает на пакет: %s", got)
	}
}
