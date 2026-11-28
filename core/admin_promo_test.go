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

// Денежный промокод создаёт и включает только админ с правом grant, сумма ограничена, помощник шлёт владельцу тревогу.
func TestMoneyPromoNeedsGrant(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	helperWith(t, e, 50, core.PermPromo)
	helperWith(t, e, 51, core.PermPromo, core.PermGrant)
	money := core.Promo{Code: "MONEY100", Kind: core.PromoMoney, Value: 10000, Active: true}
	if _, err := e.c.SavePromo(ctx, 50, money); !errors.Is(err, core.ErrForbidden) {
		t.Fatalf("помощник с promo без grant: %v", err)
	}
	if page, _ := e.c.Promos(ctx, owner, 0); len(page.Items) != 0 {
		t.Fatalf("код создан")
	}
	denied := false
	for _, a := range e.st.Audit() {
		denied = denied || a.Action == "denied:promo.save.money"
	}
	if !denied {
		t.Errorf("отказа нет в журнале")
	}
	days := core.Promo{Code: "DAYS0007", Kind: core.PromoDays, Value: 7, Active: true}
	if _, err := e.c.SavePromo(ctx, 50, days); err != nil {
		t.Fatalf("промокод на дни помощнику с promo: %v", err)
	}
	if _, err := e.c.SavePromo(ctx, 51, money); err != nil {
		t.Fatalf("с правами promo и grant: %v", err)
	}
	alert := false
	for _, n := range adminTasks(t, e) {
		alert = alert || n.Key == "admin.helper_money"
	}
	if !alert {
		t.Errorf("владелец не получил тревогу о деньгах от помощника")
	}
	for _, bad := range []int64{core.MaxPromoMoney + 1, 100000000} {
		if _, err := e.c.SavePromo(ctx, owner, core.Promo{Code: "HUGE0001", Kind: core.PromoMoney, Value: bad}); !errors.Is(err, core.ErrInvalid) {
			t.Errorf("сумма %d: %v", bad, err)
		}
	}
	if _, err := e.c.SavePromo(ctx, owner, core.Promo{Code: "DAYS9999", Kind: core.PromoDays, Value: 9999}); !errors.Is(err, core.ErrInvalid) {
		t.Errorf("9999 дней: %v", err)
	}
	// Включить денежный код помощнику без grant тоже нельзя.
	page, _ := e.c.Promos(ctx, owner, 0)
	var moneyID int64
	for _, p := range page.Items {
		if p.Kind == core.PromoMoney {
			moneyID = p.ID
		}
	}
	if _, err := e.c.SetPromoActive(ctx, 50, moneyID, false); !errors.Is(err, core.ErrForbidden) {
		t.Errorf("выключить денежный без grant: %v", err)
	}
}
