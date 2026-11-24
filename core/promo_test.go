package core_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/bini69-oi/worker/core"
)

func (e *env) promo(t *testing.T, p core.Promo) core.Promo {
	t.Helper()
	p.Active = true
	out, err := e.c.SavePromo(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestPromoMoney(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.promo(t, core.Promo{Code: "SUMMER", Kind: core.PromoMoney, Value: 10000})
	if _, err := e.c.ApplyPromo(ctx, e.user.ID, "  summer "); err != nil {
		t.Fatal(err)
	}
	hist, _ := e.c.History(ctx, e.user.ID, 1)
	if b := balance(t, e.st, e.user.ID); b != 10000 || hist[0].Reason != core.ReasonPromo || hist[0].Comment != "SUMMER" {
		t.Fatalf("баланс %d, история %+v", b, hist)
	}
	if _, err := e.c.ApplyPromo(ctx, e.user.ID, "SUMMER"); !errors.Is(err, core.ErrPromoUsed) {
		t.Fatalf("повторная активация: ждали ErrPromoUsed, получили %v", err)
	}
	if b := balance(t, e.st, e.user.ID); b != 10000 {
		t.Fatalf("повтор изменил баланс: %d", b)
	}
}

func TestPromoDays(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	sub := e.bought(t)
	e.promo(t, core.Promo{Code: "WEEK7", Kind: core.PromoDays, Value: 7})
	if _, err := e.c.ApplyPromo(ctx, e.user.ID, "week7"); err != nil {
		t.Fatal(err)
	}
	after, _ := e.c.Sub(ctx, e.user.ID)
	if !after.ExpiresAt.Equal(sub.ExpiresAt.Add(7 * 24 * time.Hour)) {
		t.Fatalf("ждали +7 дней: было %v, стало %v", sub.ExpiresAt, after.ExpiresAt)
	}
}

func TestPromoDaysWithoutSub(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	tr := e.tariff(t, "Месяц", core.Period{Days: 30, Price: 15000})
	e.promo(t, core.Promo{Code: "NOTARIFF", Kind: core.PromoDays, Value: 7})
	if _, err := e.c.ApplyPromo(ctx, e.user.ID, "NOTARIFF"); !errors.Is(err, core.ErrPromoNeedSub) {
		t.Fatalf("код без тарифа и без подписки: ждали ErrPromoNeedSub, получили %v", err)
	}
	e.promo(t, core.Promo{Code: "FREE7", Kind: core.PromoDays, Value: 7, TariffID: tr.ID})
	if _, err := e.c.ApplyPromo(ctx, e.user.ID, "FREE7"); err != nil {
		t.Fatal(err)
	}
	sub, err := e.c.Sub(ctx, e.user.ID)
	if err != nil || sub.TariffID != tr.ID || !sub.ExpiresAt.Equal(t0.Add(7*24*time.Hour)) || e.syncTasks() != 1 {
		t.Fatalf("подписка по коду не та: %+v (%v), задач %d", sub, err, e.syncTasks())
	}
}

func TestPromoLimits(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.promo(t, core.Promo{Code: "ONE", Kind: core.PromoMoney, Value: 100, MaxUses: 1})
	e.promo(t, core.Promo{Code: "OLD", Kind: core.PromoMoney, Value: 100, ExpiresAt: t0.Add(-time.Second)})
	e.promo(t, core.Promo{Code: "NEWBIE", Kind: core.PromoMoney, Value: 100, NewOnly: true})

	if _, err := e.c.ApplyPromo(ctx, e.user.ID, "ONE"); err != nil {
		t.Fatal(err)
	}
	other, _ := e.c.Register(ctx, 2002, "petr", "ru")
	if _, err := e.c.ApplyPromo(ctx, other.ID, "ONE"); !errors.Is(err, core.ErrPromoExhausted) {
		t.Fatalf("лимит исчерпан: ждали ErrPromoExhausted, получили %v", err)
	}
	if _, err := e.c.ApplyPromo(ctx, e.user.ID, "OLD"); !errors.Is(err, core.ErrPromoExpired) {
		t.Fatalf("код истёк: ждали ErrPromoExpired, получили %v", err)
	}
	e.bought(t)
	if _, err := e.c.ApplyPromo(ctx, e.user.ID, "NEWBIE"); !errors.Is(err, core.ErrPromoNewOnly) {
		t.Fatalf("старому абоненту код для новых: ждали ErrPromoNewOnly, получили %v", err)
	}
	if _, err := e.c.ApplyPromo(ctx, other.ID, "NEWBIE"); err != nil {
		t.Fatalf("новому абоненту код для новых: %v", err)
	}
}

func TestPromoBruteForce(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.promo(t, core.Promo{Code: "REAL", Kind: core.PromoMoney, Value: 100})
	for i := range 10 {
		if _, err := e.c.ApplyPromo(ctx, e.user.ID, "GUESS"+string(rune('A'+i))); !errors.Is(err, core.ErrPromoNotFound) {
			t.Fatalf("попытка %d: ждали ErrPromoNotFound, получили %v", i+1, err)
		}
	}
	if _, err := e.c.ApplyPromo(ctx, e.user.ID, "REAL"); !errors.Is(err, core.ErrTooManyTries) {
		t.Fatalf("после 10 неверных верный код должен отклоняться: %v", err)
	}
	e.clock.Add(10 * time.Minute)
	if _, err := e.c.ApplyPromo(ctx, e.user.ID, "REAL"); err != nil {
		t.Fatalf("после окна верный код должен пройти: %v", err)
	}
}

func TestPromoValidation(t *testing.T) {
	e := newEnv(t)
	for _, p := range []core.Promo{
		{Code: "AB", Kind: core.PromoMoney, Value: 1},
		{Code: "SPACE CODE", Kind: core.PromoMoney, Value: 1},
		{Code: "ZERO", Kind: core.PromoMoney, Value: 0},
		{Code: "KIND", Kind: "gift", Value: 1},
		{Code: "MONEYTARIFF", Kind: core.PromoMoney, Value: 1, TariffID: 1},
	} {
		if _, err := e.c.SavePromo(context.Background(), p); !errors.Is(err, core.ErrInvalid) {
			t.Errorf("%q: ждали ErrInvalid, получили %v", p.Code, err)
		}
	}
}
