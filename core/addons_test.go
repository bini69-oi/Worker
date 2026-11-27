package core_test

import (
	"context"
	"errors"
	"testing"

	"github.com/bini69-oi/worker/core"
)

func addonEnv(t *testing.T) (*env, core.Tariff) {
	t.Helper()
	e := newEnv(t)
	for k, v := range map[string]string{"addons.traffic": "10:15000,50:50000", "addons.device_month": "10000"} {
		if err := e.cfg.Set(k, v); err != nil {
			t.Fatal(err)
		}
	}
	tr, err := e.c.SaveTariff(context.Background(), owner, core.Tariff{Name: "Месяц", Active: true, TrafficBytes: 100 << 30,
		Devices: 3, Squads: []string{"a"}, Periods: []core.Period{{Days: 30, Price: 25000}}})
	if err != nil {
		t.Fatal(err)
	}
	e.topup(t, 100000)
	if _, err := e.c.Buy(context.Background(), e.user.ID, tr.ID, 30); err != nil {
		t.Fatal(err)
	}
	e.process(t)
	return e, tr
}

func TestBuyTraffic(t *testing.T) {
	ctx := context.Background()
	e, _ := addonEnv(t)
	before := e.balance(t)
	if _, err := e.c.BuyTraffic(ctx, e.user.ID, 10); err != nil {
		t.Fatal(err)
	}
	e.process(t)
	if e.balance(t) != before-15000 {
		t.Fatalf("за 10 ГБ списано %d", before-e.balance(t))
	}
	if pu, _ := e.panel.User("u1001"); pu.TrafficBytes != 110<<30 {
		t.Fatalf("лимит в панели %d, ждали 110 ГБ", pu.TrafficBytes)
	}
	if _, err := e.c.BuyTraffic(ctx, e.user.ID, 7); !errors.Is(err, core.ErrAddonOff) {
		t.Fatalf("пакета 7 ГБ нет: %v", err)
	}
}

func TestBuyDeviceProrated(t *testing.T) {
	ctx := context.Background()
	e, _ := addonEnv(t)
	e.clock.Add(15 * day) // осталось 15 дней из 30
	before := e.balance(t)
	if _, err := e.c.BuyDevice(ctx, e.user.ID); err != nil {
		t.Fatal(err)
	}
	e.process(t)
	if got := before - e.balance(t); got != 5000 {
		t.Fatalf("устройство на 15 дней стоило %d, ждали 5000", got)
	}
	if pu, _ := e.panel.User("u1001"); pu.Devices != 4 {
		t.Fatalf("устройств в панели %d, ждали 4", pu.Devices)
	}
}

func TestRenewWithDevices(t *testing.T) {
	ctx := context.Background()
	e, tr := addonEnv(t)
	for range 2 {
		if _, err := e.c.BuyDevice(ctx, e.user.ID); err != nil {
			t.Fatal(err)
		}
	}
	q, err := e.c.Quote(ctx, e.user.ID, tr.ID, 30)
	if err != nil || q.Price != 25000+2*10000 {
		t.Fatalf("продление с двумя устройствами: %+v %v", q, err)
	}
	before := e.balance(t)
	sub, err := e.c.Buy(ctx, e.user.ID, tr.ID, 30)
	if err != nil || before-e.balance(t) != 45000 || sub.ExtraDevices != 2 {
		t.Fatalf("продление: списано %d, устройств %d (%v)", before-e.balance(t), sub.ExtraDevices, err)
	}
}

func TestAddonsNeedLimits(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	if err := e.cfg.Set("addons.device_month", "10000"); err != nil {
		t.Fatal(err)
	}
	if err := e.cfg.Set("addons.traffic", "10:15000"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.c.BuyDevice(ctx, e.user.ID); !errors.Is(err, core.ErrNoSub) {
		t.Fatalf("без подписки: %v", err)
	}
	unl, _ := e.c.SaveTariff(ctx, owner, core.Tariff{Name: "Безлимит", Active: true, Squads: []string{"a"}, Periods: []core.Period{{Days: 30, Price: 100}}})
	e.topup(t, 1000)
	if _, err := e.c.Buy(ctx, e.user.ID, unl.ID, 30); err != nil {
		t.Fatal(err)
	}
	if _, err := e.c.BuyTraffic(ctx, e.user.ID, 10); !errors.Is(err, core.ErrUnlimited) {
		t.Fatalf("безлимит трафика: %v", err)
	}
	if _, err := e.c.BuyDevice(ctx, e.user.ID); !errors.Is(err, core.ErrUnlimited) {
		t.Fatalf("безлимит устройств: %v", err)
	}
}

func TestExtrasResetOnExpire(t *testing.T) {
	ctx := context.Background()
	e, _ := addonEnv(t)
	if _, err := e.c.BuyTraffic(ctx, e.user.ID, 10); err != nil {
		t.Fatal(err)
	}
	e.clock.Add(31 * day)
	if _, err := e.c.ExpireDue(ctx); err != nil {
		t.Fatal(err)
	}
	if s, _ := e.c.Sub(ctx, e.user.ID); s.ExtraTrafficBytes != 0 || s.ExtraDevices != 0 {
		t.Fatalf("докупленное после окончания: %+v", s)
	}
}
