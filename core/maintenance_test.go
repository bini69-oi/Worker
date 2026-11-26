package core_test

import (
	"context"
	"errors"
	"testing"

	"github.com/bini69-oi/worker/core"
)

func TestMaintenance(t *testing.T) {
	ctx := context.Background()
	e := newPayEnv(t)
	tr := e.tariff(t, "Месяц", core.Period{Days: 30, Price: 100})
	e.topup(t, 1000)
	if err := e.cfg.Set("maintenance.on", "true"); err != nil {
		t.Fatal(err)
	}
	if !e.c.Maintenance() {
		t.Fatal("техработы не видны")
	}

	if _, err := e.c.Buy(ctx, e.user.ID, tr.ID, 30); !errors.Is(err, core.ErrMaintenance) {
		t.Fatalf("покупка: ждали ErrMaintenance, получили %v", err)
	}
	if _, err := e.c.Trial(ctx, e.user.ID); !errors.Is(err, core.ErrMaintenance) {
		t.Fatalf("пробный: ждали ErrMaintenance, получили %v", err)
	}
	if _, err := e.c.ApplyPromo(ctx, e.user.ID, "SPRING26"); !errors.Is(err, core.ErrMaintenance) {
		t.Fatalf("промокод: ждали ErrMaintenance, получили %v", err)
	}

	// Счёт, созданный до техработ, зачисляется во время них.
	if err := e.cfg.Set("maintenance.on", "false"); err != nil {
		t.Fatal(err)
	}
	p, _, err := e.c.CreatePayment(ctx, e.user.ID, "fake", 30000)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.cfg.Set("maintenance.on", "true"); err != nil {
		t.Fatal(err)
	}
	before := e.balance(t)
	if err := e.c.Credit(ctx, "fake", core.GatewayEvent{ExternalID: p.ExternalID, ExtAmount: 30000, Status: core.PayPaid}); err != nil {
		t.Fatalf("зачисление во время техработ: %v", err)
	}
	if e.balance(t) != before+30000 {
		t.Fatal("оплата не зачислена во время техработ")
	}
}

func TestMaintenanceLetsAdminsThrough(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	tr := e.tariff(t, "Месяц", core.Period{Days: 30, Price: 100})
	if err := e.cfg.Set("maintenance.on", "true"); err != nil {
		t.Fatal(err)
	}
	admin, err := e.c.Register(ctx, owner, "boss", "ru")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.c.AdminAdjust(ctx, owner, admin.ID, 1000, "тест"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.c.Buy(ctx, admin.ID, tr.ID, 30); err != nil {
		t.Fatalf("владелец во время техработ: %v", err)
	}
}
