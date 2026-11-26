package core_test

import (
	"context"
	"testing"
)

func TestSupportCard(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	card, err := e.c.SupportCard(ctx, e.user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if card.User.TgID != 1001 || card.Tariff != "" || card.Active {
		t.Fatalf("без подписки: %+v", card)
	}

	sub := e.bought(t)
	card, err = e.c.SupportCard(ctx, e.user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if card.Tariff != "Месяц" || !card.Active || !card.ExpiresAt.Equal(sub.ExpiresAt) || card.User.Balance != e.balance(t) {
		t.Fatalf("с подпиской: %+v", card)
	}
}
