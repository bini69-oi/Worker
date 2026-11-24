package core_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/bini69-oi/worker/core"
)

func TestSyncRightAfterBuy(t *testing.T) {
	e := newEnv(t)
	sub := e.bought(t)
	pu, ok := e.panel.User("u1001")
	if !ok || !pu.Enabled || !pu.ExpireAt.Equal(sub.ExpiresAt) || len(pu.Squads) != 2 {
		t.Fatalf("в панели не то: %+v (есть: %v)", pu, ok)
	}
	if sub.PanelUUID == "" || sub.SubURL == "" {
		t.Fatalf("ссылка и идентификатор панели не сохранены: %+v", sub)
	}
	select {
	case <-e.c.Kick():
	default:
		t.Fatal("после покупки воркер очереди не разбужен")
	}
}

func TestPanelDownTenMinutes(t *testing.T) {
	e := newEnv(t)
	e.panel.SetDown(true)
	tr := e.tariff(t, "Месяц", core.Period{Days: 30, Price: 100})
	e.topup(t, 1000)
	sub, err := e.c.Buy(context.Background(), e.user.ID, tr.ID, 30)
	if err != nil {
		t.Fatalf("покупка не должна зависеть от панели: %v", err)
	}
	for range 10 {
		e.process(t)
		e.clock.Add(time.Minute)
	}
	if _, ok := e.panel.User("u1001"); ok {
		t.Fatal("абонент в лежащей панели")
	}
	e.panel.SetDown(false)
	e.clock.Add(10 * time.Minute) // следующая попытка по паузе
	e.process(t)
	pu, ok := e.panel.User("u1001")
	if !ok || !pu.ExpireAt.Equal(sub.ExpiresAt) {
		t.Fatalf("после подъёма панели абонент не появился: %+v", pu)
	}
	if n := e.st.Pending(core.KindPanelSync); n != 0 {
		t.Fatalf("в очереди осталось задач синхронизации: %d", n)
	}
}

func TestBackoffSteps(t *testing.T) {
	want := []time.Duration{5 * time.Second, 30 * time.Second, 2 * time.Minute, 10 * time.Minute, 30 * time.Minute, 30 * time.Minute}
	for i, w := range want {
		if got := core.Backoff(i + 1); got != w {
			t.Errorf("попытка %d: пауза %v, ждали %v", i+1, got, w)
		}
	}
}

func TestStuckTasksAlertOnce(t *testing.T) {
	e := newEnv(t)
	e.panel.SetDown(true)
	tr := e.tariff(t, "Месяц", core.Period{Days: 30, Price: 100})
	e.topup(t, 1000)
	if _, err := e.c.Buy(context.Background(), e.user.ID, tr.ID, 30); err != nil {
		t.Fatal(err)
	}
	for range 13 { // 2 часа 10 минут шагами по 10 минут
		e.clock.Add(10 * time.Minute)
		e.process(t)
	}
	stuck := 0
	for _, a := range e.notes.ToAdmins {
		if a.Key == "admin.tasks_stuck" {
			stuck++
			if a.Vars["count"] != "1" || a.Topic != core.TopicErrors {
				t.Fatalf("тревога не та: %+v", a)
			}
		}
	}
	if stuck != 1 {
		t.Fatalf("ждали одну тревогу о зависших задачах, получили %d", stuck)
	}
}

func TestSyncTwiceSameExpire(t *testing.T) {
	e := newEnv(t)
	sub := e.bought(t)
	if err := e.c.SyncSub(context.Background(), sub.ID); err != nil {
		t.Fatal(err)
	}
	pu, _ := e.panel.User("u1001")
	if !pu.ExpireAt.Equal(sub.ExpiresAt) {
		t.Fatalf("повтор задачи продлил дважды: в панели %v, в базе %v", pu.ExpireAt, sub.ExpiresAt)
	}
}

func TestLinkSameAfterRenew(t *testing.T) {
	e := newEnv(t)
	first := e.bought(t)
	tariffs, _ := e.c.Tariffs(context.Background())
	if _, err := e.c.Buy(context.Background(), e.user.ID, tariffs[0].ID, 30); err != nil {
		t.Fatal(err)
	}
	e.process(t)
	renewed, _ := e.c.Sub(context.Background(), e.user.ID)
	if renewed.SubURL != first.SubURL || renewed.PanelUUID != first.PanelUUID {
		t.Fatalf("ссылка поменялась при продлении: %s и %s", first.SubURL, renewed.SubURL)
	}
	pu, _ := e.panel.User("u1001")
	if !pu.ExpireAt.Equal(renewed.ExpiresAt) {
		t.Fatalf("в панели срок %v, в базе %v", pu.ExpireAt, renewed.ExpiresAt)
	}
}

func TestTrialLimitsFromSettings(t *testing.T) {
	e := newEnv(t)
	if err := e.cfg.Set("trial.squads", "s1, s2"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.c.Trial(context.Background(), e.user.ID); err != nil {
		t.Fatal(err)
	}
	e.process(t)
	pu, _ := e.panel.User("u1001")
	if pu.TrafficBytes != 5<<30 || pu.Devices != 1 || len(pu.Squads) != 2 || pu.Squads[1] != "s2" {
		t.Fatalf("лимиты пробного периода не из настроек: %+v", pu)
	}
}

func TestExpiredDisabledInPanel(t *testing.T) {
	e := newEnv(t)
	e.bought(t)
	e.clock.Add(31 * 24 * time.Hour)
	if _, err := e.c.ExpireDue(context.Background()); err != nil {
		t.Fatal(err)
	}
	e.process(t)
	if pu, _ := e.panel.User("u1001"); pu.Enabled {
		t.Fatal("абонент с вышедшим сроком включён в панели")
	}
}

func TestNotifyDelivered(t *testing.T) {
	e := newEnv(t)
	e.bought(t)
	e.clock.Add(27 * 24 * time.Hour)
	if _, err := e.c.RemindDue(context.Background()); err != nil {
		t.Fatal(err)
	}
	e.process(t)
	if len(e.notes.ToUsers) != 1 || e.notes.ToUsers[0].Key != "notify.remind" || e.notes.ToUsers[0].UserID != e.user.ID {
		t.Fatalf("напоминание не доставлено: %+v", e.notes.ToUsers)
	}
}

func TestNoNotifierKeepsTasks(t *testing.T) {
	e := newEnv(t)
	c := core.New(core.Deps{Store: e.st, Clock: e.clock, Config: e.cfg, Panel: e.panel, OwnerID: owner})
	e.bought(t)
	e.clock.Add(27 * 24 * time.Hour)
	if _, err := c.RemindDue(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := c.ProcessDue(context.Background()); err != nil {
		t.Fatal(err)
	}
	if n := e.st.Pending(core.KindNotifyUser); n != 1 {
		t.Fatalf("без бота уведомление должно ждать в очереди, ждут %d", n)
	}
}

func TestReconcileRevertsManualEdit(t *testing.T) {
	e := newEnv(t)
	sub := e.bought(t)
	e.panel.Edit("u1001", func(u *core.PanelUser) { u.ExpireAt = u.ExpireAt.Add(30 * 24 * time.Hour) })

	n, err := e.c.Reconcile(context.Background())
	if err != nil || n != 1 {
		t.Fatalf("ждали одно расхождение, получили %d (%v)", n, err)
	}
	e.process(t)
	pu, _ := e.panel.User("u1001")
	if !pu.ExpireAt.Equal(sub.ExpiresAt) {
		t.Fatalf("сверка не вернула срок: в панели %v, в базе %v", pu.ExpireAt, sub.ExpiresAt)
	}
	if len(e.notes.ToAdmins) != 1 || e.notes.ToAdmins[0].Key != "admin.panel_diff" {
		t.Fatalf("админ не увидел расхождение: %+v", e.notes.ToAdmins)
	}
	if n, _ := e.c.Reconcile(context.Background()); n != 0 {
		t.Fatalf("после исправления остались расхождения: %d", n)
	}
}

func TestDevicesAndRevoke(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	if _, err := e.c.Devices(ctx, e.user.ID); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("без подписки: ждали ErrNotFound, получили %v", err)
	}
	sub := e.bought(t)
	e.panel.AddDevice(sub.PanelUUID, "a")
	e.panel.AddDevice(sub.PanelUUID, "b")
	if err := e.c.DeleteDevice(ctx, e.user.ID, "a"); err != nil {
		t.Fatal(err)
	}
	if list, _ := e.c.Devices(ctx, e.user.ID); len(list) != 1 || list[0].HWID != "b" {
		t.Fatalf("после удаления одного: %+v", list)
	}
	if err := e.c.ResetDevices(ctx, e.user.ID); err != nil {
		t.Fatal(err)
	}
	if list, _ := e.c.Devices(ctx, e.user.ID); len(list) != 0 {
		t.Fatalf("после сброса остались устройства: %+v", list)
	}
	fresh, err := e.c.RevokeLink(ctx, e.user.ID)
	if err != nil {
		t.Fatal(err)
	}
	saved, _ := e.c.Sub(ctx, e.user.ID)
	if fresh.SubURL == sub.SubURL || saved.SubURL != fresh.SubURL {
		t.Fatalf("новая ссылка не выдана или не сохранена: было %s, стало %s, в базе %s", sub.SubURL, fresh.SubURL, saved.SubURL)
	}
	e.panel.SetDown(true)
	if err := e.c.ResetDevices(ctx, e.user.ID); !errors.Is(err, core.ErrPanelDown) {
		t.Fatalf("панель лежит: ждали ErrPanelDown, получили %v", err)
	}
}

func TestDevicesBeforePanel(t *testing.T) {
	e := newEnv(t)
	e.panel.SetDown(true)
	tr := e.tariff(t, "Месяц", core.Period{Days: 30, Price: 0})
	if _, err := e.c.Buy(context.Background(), e.user.ID, tr.ID, 30); err != nil {
		t.Fatal(err)
	}
	if _, err := e.c.Devices(context.Background(), e.user.ID); !errors.Is(err, core.ErrPanelPending) {
		t.Fatalf("подписка ещё в очереди: ждали ErrPanelPending, получили %v", err)
	}
}

func TestReconcileBannedNoDiff(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.bought(t)
	u := e.user
	u.BannedAt = t0
	e.st.SetUser(u)
	sub, _ := e.c.Sub(ctx, e.user.ID)
	if err := e.c.SyncSub(ctx, sub.ID); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if n, err := e.c.Reconcile(ctx); err != nil || n != 0 {
			t.Fatalf("забаненный абонент считается расхождением: %d (%v)", n, err)
		}
	}
}
