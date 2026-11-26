package core_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/bini69-oi/worker/core"
)

func notifyTasks(t *testing.T, e *env) []core.NotifyTask {
	t.Helper()
	var out []core.NotifyTask
	for _, task := range e.st.Tasks() {
		if task.Kind != core.KindNotifyUser {
			continue
		}
		var n core.NotifyTask
		if err := json.Unmarshal(task.Payload, &n); err != nil {
			t.Fatal(err)
		}
		out = append(out, n)
	}
	return out
}

func TestExpireDue(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	if _, err := e.c.Trial(ctx, e.user.ID); err != nil {
		t.Fatal(err)
	}
	if n, _ := e.c.ExpireDue(ctx); n != 0 {
		t.Fatalf("живая подписка отключена: %d", n)
	}
	e.clock.Add(3 * 24 * time.Hour)
	before := e.syncTasks()
	if n, err := e.c.ExpireDue(ctx); err != nil || n != 1 {
		t.Fatalf("ждали одну отключённую, получили %d (%v)", n, err)
	}
	sub, _ := e.c.Sub(ctx, e.user.ID)
	if sub.Status != core.SubExpired || e.syncTasks() != before+1 {
		t.Fatalf("подписка %s, задач синхронизации +%d", sub.Status, e.syncTasks()-before)
	}
	if n, _ := e.c.ExpireDue(ctx); n != 0 {
		t.Fatalf("повторный запуск отключил ещё %d", n)
	}
	expired := 0
	for _, n := range notifyTasks(t, e) {
		if n.Key == "notify.expired" {
			expired++
			if n.Vars["trial"] != "1" {
				t.Fatalf("пробный не отмечен в уведомлении: %+v", n)
			}
		}
	}
	if expired != 1 {
		t.Fatalf("уведомлений «подписка закончилась» %d, ждали 1", expired)
	}
}

func TestRemindOncePerMark(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	tr := e.tariff(t, "Месяц", core.Period{Days: 30, Price: 100})
	e.topup(t, 1000)
	if _, err := e.c.Buy(ctx, e.user.ID, tr.ID, 30); err != nil {
		t.Fatal(err)
	}

	e.clock.Add(26 * 24 * time.Hour) // осталось 4 дня
	if n, _ := e.c.RemindDue(ctx); n != 0 {
		t.Fatalf("рано напомнили: %d", n)
	}
	e.clock.Add(24 * time.Hour) // осталось 3 дня
	if n, _ := e.c.RemindDue(ctx); n != 1 {
		t.Fatalf("за 3 дня ждали одно напоминание, получили %d", n)
	}
	e.clock.Add(time.Hour)
	if n, _ := e.c.RemindDue(ctx); n != 0 {
		t.Fatalf("повтор в тот же день: %d", n)
	}
	e.clock.Add(47 * time.Hour) // осталось 1 день
	if n, _ := e.c.RemindDue(ctx); n != 1 {
		t.Fatalf("за 1 день ждали одно напоминание, получили %d", n)
	}

	got := notifyTasks(t, e)
	if len(got) != 2 || got[0].Vars["days"] != "3" || got[1].Vars["days"] != "1" || got[0].Key != "notify.remind" {
		t.Fatalf("напоминания не те: %+v", got)
	}
}

func TestRemindRestartsAfterRenew(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	tr := e.tariff(t, "Месяц", core.Period{Days: 30, Price: 100})
	e.topup(t, 1000)
	if _, err := e.c.Buy(ctx, e.user.ID, tr.ID, 30); err != nil {
		t.Fatal(err)
	}
	e.clock.Add(27 * 24 * time.Hour)
	if n, _ := e.c.RemindDue(ctx); n != 1 {
		t.Fatalf("ждали напоминание, получили %d", n)
	}
	if _, err := e.c.Buy(ctx, e.user.ID, tr.ID, 30); err != nil {
		t.Fatal(err)
	}
	e.clock.Add(30 * 24 * time.Hour) // снова 3 дня до нового окончания
	if n, _ := e.c.RemindDue(ctx); n != 1 {
		t.Fatalf("после продления напоминания должны начаться заново, получили %d", n)
	}
}
