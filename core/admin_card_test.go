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

func TestAdminDevicesAndLink(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	sub := e.bought(t)
	e.panel.AddDevice(sub.PanelUUID, "hw1")
	helperWith(t, e, 50, core.PermUsers)
	if err := e.c.AdminResetDevices(ctx, 50, e.user.ID); !errors.Is(err, core.ErrForbidden) {
		t.Fatalf("без права grant: %v", err)
	}
	if err := e.c.AdminResetDevices(ctx, owner, e.user.ID); err != nil {
		t.Fatal(err)
	}
	if d, _ := e.c.Devices(ctx, e.user.ID); len(d) != 0 {
		t.Errorf("устройства остались: %v", d)
	}
	before, _ := e.c.Sub(ctx, e.user.ID)
	if err := e.c.AdminRevokeLink(ctx, owner, e.user.ID); err != nil {
		t.Fatal(err)
	}
	if after, _ := e.c.Sub(ctx, e.user.ID); after.SubURL == before.SubURL {
		t.Errorf("ссылка не сменилась")
	}
	log := e.st.Audit()
	if a := log[len(log)-1]; a.Action != "link.revoke" {
		t.Errorf("журнал: %+v", a)
	}
	e.panel.SetDown(true)
	if err := e.c.AdminResetDevices(ctx, owner, e.user.ID); !errors.Is(err, core.ErrPanelDown) {
		t.Errorf("панель лежит: %v", err)
	}
	// Панель не ответила: в журнале остался запрос, итога нет.
	var requests, results int
	for _, a := range e.st.Audit() {
		if a.Action == "devices.reset.request" {
			requests++
		}
		if a.Action == "devices.reset" {
			results++
		}
	}
	if requests != 2 || results != 1 {
		t.Errorf("журнал: запросов %d, итогов %d", requests, results)
	}
	if err := e.c.AdminResetDevices(ctx, owner, 9999); err == nil {
		t.Errorf("нет такого абонента, а ошибки нет")
	}
}

// Карточка абонента показывает только записи о нём: действие над платежом, тарифом или настройкой с тем же
// числом в target не попадает в чужую карточку.
func TestUserCardAuditOnlyAboutUser(t *testing.T) {
	ctx := context.Background()
	e := newCfgEnv(t)
	helperWith(t, e.env, 50, core.PermUsers)
	if err := e.st.Tx(ctx, func(tx core.Tx) error {
		for _, a := range []core.AuditEntry{
			{AdminTg: owner, Action: "payment.confirm", Target: itoa(int(e.user.ID)), At: e.clock.Now()},
			{AdminTg: owner, Action: "tariff.save", Target: itoa(int(e.user.ID)), At: e.clock.Now()},
			{AdminTg: owner, Action: "setting.set", Target: "stars.rate", Details: map[string]string{"new": "999"}, At: e.clock.Now()},
		} {
			if err := tx.AddAudit(a); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.c.AdminAdjust(ctx, owner, e.user.ID, 100, "своя запись"); err != nil {
		t.Fatal(err)
	}
	card, err := e.c.UserCard(ctx, 50, e.user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(card.Audit) != 1 || card.Audit[0].Action != "balance.adjust" {
		t.Errorf("в карточке чужие записи: %+v", card.Audit)
	}
}
