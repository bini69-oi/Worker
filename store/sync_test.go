package store

import (
	"context"
	"testing"
	"time"

	"github.com/bini69-oi/worker/core"
	"github.com/bini69-oi/worker/core/coretest"
	"github.com/bini69-oi/worker/panel"
	"github.com/bini69-oi/worker/panel/paneltest"
)

// TestSyncWithRealPanelClient прогоняет путь целиком на настоящих частях: ядро, SQLite, HTTP-клиент панели
// и фейковая панель 3.x. Покупка доходит до панели, ручная правка в панели откатывается сверкой.
func TestSyncWithRealPanelClient(t *testing.T) {
	s, uid := newCoreStore(t)
	ctx := context.Background()
	fake := paneltest.New(t, paneltest.V3)
	clock := coretest.NewClock(time.Now().UTC().Truncate(time.Second))
	c := core.New(core.Deps{
		Store: s, Clock: clock, Config: coretest.NewConfig(t),
		Panel:   panel.CoreAdapter{Client: panel.New(fake.URL(), paneltest.Token, nil)},
		OwnerID: 1,
	})

	tr, err := c.SaveTariff(ctx, 1, core.Tariff{Name: "Месяц", Active: true, Squads: []string{"11111111-1111-1111-1111-111111111111"},
		Periods: []core.Period{{Days: 30, Price: 0}}})
	if err != nil {
		t.Fatal(err)
	}
	sub, err := c.Buy(ctx, uid, tr.ID, 30)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.ProcessDue(ctx); err != nil {
		t.Fatal(err)
	}
	users := fake.Users()
	if len(users) != 1 || users[0].Username != "u1001" || users[0].Status != "ACTIVE" || !users[0].ExpireAt.Equal(sub.ExpiresAt) {
		t.Fatalf("в панели не то: %+v", users)
	}
	saved, _ := s.SubByUser(ctx, uid)
	if saved.PanelUUID == "" || saved.SubURL == "" {
		t.Fatalf("идентификатор и ссылка панели не сохранены: %+v", saved)
	}

	fake.Edit("u1001", func(u *paneltest.User) { u.ExpireAt = u.ExpireAt.Add(30 * 24 * time.Hour) })
	if n, err := c.Reconcile(ctx); err != nil || n != 1 {
		t.Fatalf("сверка: ждали одно расхождение, получили %d (%v)", n, err)
	}
	if _, err := c.ProcessDue(ctx); err != nil {
		t.Fatal(err)
	}
	if got := fake.Users()[0].ExpireAt; !got.Equal(sub.ExpiresAt) {
		t.Fatalf("сверка не вернула срок: %v вместо %v", got, sub.ExpiresAt)
	}
}
