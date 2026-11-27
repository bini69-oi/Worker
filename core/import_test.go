package core_test

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/bini69-oi/worker/core"
)

const gb = 1 << 30

// listed кладёт в фейковую панель пользователя с лимитом 50 ГБ, 3 устройства, до 15.03.
func (e *env) listed(ref, username string, tg int64, created int) core.PanelEntry {
	en := core.PanelEntry{Ref: ref, Username: username, TgID: tg, Status: "ACTIVE",
		ExpireAt: t0.AddDate(0, 2, 14), CreatedAt: t0.AddDate(0, 0, -100+created), SubURL: "https://sub.example/" + ref,
		TrafficBytes: 50 * gb, Devices: 3, Reset: "MONTH", Squads: []string{"sq-1"}}
	e.panel.AddListed(en)
	return en
}

func TestImportDryRunWritesNothing(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	e.listed("r1", "old_one", 2001, 0)
	e.listed("r2", "no_tg", 0, 1)
	e.panel.AddListed(core.PanelEntry{Ref: "r3", Username: "banned_there", TgID: 2003, Status: "DISABLED", ExpireAt: t0.AddDate(0, 1, 0)})
	e.panel.AddSkipped(core.PanelSkip{Username: "junk", Reason: "плохая дата окончания"})

	rep, err := e.c.ImportPanel(ctx, owner, false)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Applied || rep.Subs != 1 || rep.NewUsers != 1 {
		t.Fatalf("отчёт: %+v", rep)
	}
	for _, reason := range []string{"нет Telegram ID", "отключён в панели", "плохая дата окончания"} {
		if rep.Reasons[reason] == 0 {
			t.Fatalf("нет причины %q: %+v", reason, rep.Reasons)
		}
	}
	if _, err := e.c.UserByTg(ctx, 2001); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("пробный прогон завёл абонента: %v", err)
	}
	if len(e.st.Audit()) != 0 || len(e.st.Tasks()) != 0 {
		t.Fatal("пробный прогон оставил журнал или задачи")
	}
}

func TestImportKeepsLimitsAfterSync(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	en := e.listed("r1", "old_one", 2001, 0)
	if _, err := e.c.ImportPanel(ctx, owner, true); err != nil {
		t.Fatal(err)
	}
	u, err := e.c.UserByTg(ctx, 2001)
	if err != nil {
		t.Fatal(err)
	}
	sub, err := e.c.Sub(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !sub.Imported || !sub.ExpiresAt.Equal(en.ExpireAt) || sub.SubURL != en.SubURL || sub.Status != core.SubActive || !u.TrialUsed {
		t.Fatalf("подписка: %+v, пробный взят: %v", sub, u.TrialUsed)
	}

	// Кто-то сдвинул срок в панели: сверка возвращает его, лимиты и имя при этом не меняются.
	e.panel.Edit("old_one", func(p *core.PanelUser) { p.ExpireAt = p.ExpireAt.AddDate(0, 1, 0) })
	if n, err := e.c.Reconcile(ctx); err != nil || n != 1 {
		t.Fatalf("сверка: %d %v", n, err)
	}
	e.process(t)
	p, ok := e.panel.User("old_one")
	if !ok {
		t.Fatal("пользователь переименован в панели")
	}
	if p.Ref != "r1" || p.TrafficBytes != 50*gb || p.Devices != 3 || p.Reset != "MONTH" || !slices.Equal(p.Squads, []string{"sq-1"}) ||
		!p.ExpireAt.Equal(en.ExpireAt) || !p.Enabled {
		t.Fatalf("панель после сверки: %+v", p)
	}
}

func TestImportTwiceNoDuplicates(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	e.listed("r1", "a", 2001, 0)
	e.listed("r2", "b", 2002, 1)
	if _, err := e.c.ImportPanel(ctx, owner, true); err != nil {
		t.Fatal(err)
	}
	rep, err := e.c.ImportPanel(ctx, owner, true)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Subs != 0 || rep.Reasons["уже перенесён"] != 2 {
		t.Fatalf("второй импорт: %+v", rep)
	}
}

func TestImportMatching(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	e.bought(t) // у ivan (1001) уже есть подписка
	free, err := e.c.Register(ctx, 3001, "petr", "ru")
	if err != nil {
		t.Fatal(err)
	}
	e.listed("late", "dup_late", 2001, 5)
	e.listed("early", "dup_early", 2001, 1)
	e.listed("has", "has_sub", 1001, 2)
	e.listed("free", "free_user", 3001, 3)

	rep, err := e.c.ImportPanel(ctx, owner, true)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Subs != 2 || rep.NewUsers != 1 || rep.Reasons["уже есть подписка"] != 1 || rep.Reasons["Telegram ID уже у другого пользователя панели"] != 1 {
		t.Fatalf("отчёт: %+v", rep)
	}
	u, _ := e.c.UserByTg(ctx, 2001)
	if sub, _ := e.c.Sub(ctx, u.ID); sub.PanelUUID != "early" {
		t.Fatalf("при одном Telegram ID взят не самый ранний: %q", sub.PanelUUID)
	}
	if sub, err := e.c.Sub(ctx, free.ID); err != nil || sub.PanelUUID != "free" {
		t.Fatalf("абонент без подписки не получил её: %+v %v", sub, err)
	}
}

func TestImportOwnerOnly(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	e.listed("r1", "a", 2001, 0)
	if _, err := e.c.ImportPanel(ctx, 777, true); !errors.Is(err, core.ErrOwnerOnly) {
		t.Fatalf("помощник запустил импорт: %v", err)
	}
	if _, err := e.c.UserByTg(ctx, 2001); !errors.Is(err, core.ErrNotFound) {
		t.Fatal("отказ всё равно завёл абонента")
	}
	if a := e.st.Audit(); len(a) != 1 || a[0].Action != "denied:import.panel" {
		t.Fatalf("журнал: %+v", a)
	}
	e.process(t)
	if len(e.notes.ToOwner) != 1 || e.notes.ToOwner[0].Key != "admin.denied" {
		t.Fatalf("тревога владельцу: %+v", e.notes.ToOwner)
	}
}

func TestImportAuditAndAlert(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	e.listed("r1", "a", 2001, 0)
	e.listed("r2", "b", 0, 1)
	if _, err := e.c.ImportPanel(ctx, owner, true); err != nil {
		t.Fatal(err)
	}
	a := e.st.Audit()
	if len(a) != 1 || a[0].Action != "import.panel" || a[0].Details["subs"] != "1" || a[0].Details["skipped"] != "1" {
		t.Fatalf("журнал: %+v", a)
	}
	e.process(t)
	if len(e.notes.ToOwner) != 1 || e.notes.ToOwner[0].Key != "admin.imported" || e.notes.ToOwner[0].Vars["users"] != "1" {
		t.Fatalf("тревога с итогом: %+v", e.notes.ToOwner)
	}
	for _, n := range e.notes.ToAdmins {
		if n.Key == "admin.new_user" {
			t.Fatal("импорт прислал событие о каждом новом абоненте")
		}
	}
}

// Купил месяц при перенесённой подписке до 15.03: до 15.03 лимиты панели, с 15.03 тариф на 30 дней.
func TestImportedBuyQueuesPlan(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	en := e.listed("r1", "old_one", 1001, 0) // ivan
	if _, err := e.c.ImportPanel(ctx, owner, true); err != nil {
		t.Fatal(err)
	}
	month := e.tariff(t, "Месяц", core.Period{Days: 30, Price: 100})
	e.topup(t, 500)
	sub, err := e.c.Buy(ctx, e.user.ID, month.ID, 30)
	if err != nil {
		t.Fatal(err)
	}
	if !sub.Imported || sub.NextTariffID != month.ID || !sub.ExpiresAt.Equal(en.ExpireAt) {
		t.Fatalf("покупка не встала сменой: %+v", sub)
	}
	e.clock.Add(en.ExpireAt.Sub(e.clock.Now()) + time.Minute)
	if _, err := e.c.ExpireDue(ctx); err != nil {
		t.Fatal(err)
	}
	e.process(t)
	got, _ := e.c.Sub(ctx, e.user.ID)
	if got.Imported || got.TariffID != month.ID || !got.ExpiresAt.Equal(en.ExpireAt.AddDate(0, 0, 30)) || got.Status != core.SubActive {
		t.Fatalf("с 15.03 не включился тариф: %+v", got)
	}
	p, ok := e.panel.User("old_one")
	if !ok || p.Ref != "r1" || p.TrafficBytes != month.TrafficBytes || p.Devices != month.Devices {
		t.Fatalf("панель после смены: %+v %v", p, ok)
	}
}
