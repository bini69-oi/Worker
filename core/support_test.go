package core_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/bini69-oi/worker/core"
)

func TestSupportCard(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	card, err := e.c.SupportCard(ctx, e.user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if card.User.TgID != 1001 || card.Tariff != "" || !card.ExpiresAt.IsZero() {
		t.Fatalf("без подписки: %+v", card)
	}

	sub := e.bought(t)
	card, err = e.c.SupportCard(ctx, e.user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if card.Tariff != "Месяц" || !card.ExpiresAt.Equal(sub.ExpiresAt) || card.User.Balance != e.balance(t) {
		t.Fatalf("с подпиской: %+v", card)
	}
}

func TestSupportLinks(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	if err := e.c.LinkSupport(ctx, -5, 77, e.user.ID); err != nil {
		t.Fatal(err)
	}
	if uid, err := e.c.SupportUser(ctx, -5, 77); err != nil || uid != e.user.ID {
		t.Fatalf("связь: %d %v", uid, err)
	}
	if _, err := e.c.SupportUser(ctx, -5, 78); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("нет связи: %v", err)
	}
	e.clock.Add(31 * 24 * time.Hour)
	if _, err := e.c.SupportUser(ctx, -5, 77); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("связь старше 30 дней работает: %v", err)
	}
	if n, err := e.c.CleanSupport(ctx); err != nil || n != 1 {
		t.Fatalf("очистка: %d %v", n, err)
	}
}
