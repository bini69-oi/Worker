package core_test

import (
	"context"
	"errors"
	"testing"

	"github.com/bini69-oi/worker/core"
)

func TestUserCard(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	sub := e.bought(t)
	e.panel.AddDevice(sub.PanelUUID, "hw1")
	if _, err := e.c.AdminAdjust(ctx, owner, e.user.ID, 5000, "компенсация"); err != nil {
		t.Fatal(err)
	}
	inv, err := e.c.Register(ctx, 4001, "friend", "ru")
	if err != nil {
		t.Fatal(err)
	}
	_ = inv

	card, err := e.c.UserCard(ctx, owner, e.user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if card.User.TgID != 1001 || card.Sub == nil || card.Tariff != "Месяц" || len(card.Ledger) < 2 {
		t.Fatalf("карточка: %+v", card)
	}
	if !card.DevicesOK || len(card.Devices) != 1 || card.Devices[0].HWID != "hw1" {
		t.Errorf("устройства: %+v ok=%v", card.Devices, card.DevicesOK)
	}
	foundAudit := false
	for _, a := range card.Audit {
		foundAudit = foundAudit || a.Action == "balance.adjust"
	}
	if !foundAudit {
		t.Errorf("в карточке нет записей журнала: %+v", card.Audit)
	}
	// Реферал видит пригласившего.
	friend, _ := e.c.UserByTg(ctx, 4001)
	if c2, err := e.c.UserCard(ctx, owner, friend.ID); err != nil || c2.Sub != nil || c2.Referrer != nil {
		t.Errorf("абонент без подписки: %+v %v", c2, err)
	}
}

func TestUserCardPanelDown(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	e.bought(t)
	e.panel.SetDown(true)
	card, err := e.c.UserCard(ctx, owner, e.user.ID)
	if err != nil {
		t.Fatalf("карточка не должна падать вместе с панелью: %v", err)
	}
	if card.DevicesOK || len(card.Devices) != 0 || card.User.ID != e.user.ID || card.Sub == nil {
		t.Errorf("панель лежит: %+v", card)
	}
}

func TestUserCardRights(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	helperWith(t, e, 50, core.PermSupport)
	if _, err := e.c.UserCard(ctx, 50, e.user.ID); !errors.Is(err, core.ErrForbidden) {
		t.Errorf("помощник без права: %v", err)
	}
	if _, err := e.c.UserCard(ctx, owner, 9999); !errors.Is(err, core.ErrNotFound) {
		t.Errorf("нет такого абонента: %v", err)
	}
}
