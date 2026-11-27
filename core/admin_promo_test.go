package core_test

import (
	"context"
	"errors"
	"testing"

	"github.com/bini69-oi/worker/core"
)

func TestPromosWithUses(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	p := e.promo(t, core.Promo{Code: "SPRING26", Kind: core.PromoDays, Value: 7, MaxUses: 100, TariffID: e.tariff(t, "Месяц", core.Period{Days: 30, Price: 100}).ID})
	e.promo(t, core.Promo{Code: "OTHER26", Kind: core.PromoMoney, Value: 5000})
	for tg := int64(2001); tg <= 2002; tg++ {
		u, err := e.c.Register(ctx, tg, "u", "ru")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := e.c.ApplyPromo(ctx, u.ID, "SPRING26"); err != nil {
			t.Fatal(err)
		}
	}
	page, err := e.c.Promos(ctx, owner, 0)
	if err != nil || len(page.Items) != 2 || page.Next != 0 {
		t.Fatalf("%+v %v", page, err)
	}
	if page.Items[0].Code != "OTHER26" || page.Items[1].Code != "SPRING26" || page.Items[1].Uses != 2 || page.Items[1].ID != p.ID {
		t.Errorf("порядок или число активаций: %+v", page.Items)
	}
	next, _ := e.c.Promos(ctx, owner, page.Items[0].ID)
	if len(next.Items) != 1 || next.Items[0].Code != "SPRING26" {
		t.Errorf("курсор: %+v", next.Items)
	}
}

func TestPromosRightsAndActive(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	p := e.promo(t, core.Promo{Code: "SPRING26", Kind: core.PromoMoney, Value: 5000})
	helperWith(t, e, 50, core.PermUsers)
	if _, err := e.c.Promos(ctx, 50, 0); !errors.Is(err, core.ErrForbidden) {
		t.Errorf("помощник без promo: %v", err)
	}
	if _, err := e.c.SetPromoActive(ctx, 50, p.ID, false); !errors.Is(err, core.ErrForbidden) {
		t.Errorf("выключение без права: %v", err)
	}
	off, err := e.c.SetPromoActive(ctx, owner, p.ID, false)
	if err != nil || off.Active {
		t.Fatalf("%+v %v", off, err)
	}
	if _, err := e.c.ApplyPromo(ctx, e.user.ID, "SPRING26"); !errors.Is(err, core.ErrPromoNotFound) {
		t.Errorf("выключенный код сработал: %v", err)
	}
	if _, err := e.c.SetPromoActive(ctx, owner, 9999, true); !errors.Is(err, core.ErrNotFound) {
		t.Errorf("нет такого кода: %v", err)
	}
}
