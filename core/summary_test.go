package core_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/bini69-oi/worker/core"
	"github.com/bini69-oi/worker/core/coretest"
)

func summaryTasks(t *testing.T, e *cfgEnv) []core.AdminTask {
	t.Helper()
	var out []core.AdminTask
	for _, n := range adminTasks(t, e.env) {
		if n.Topic == core.TopicSummary && n.Key == "admin.summary" {
			out = append(out, n)
		}
	}
	return out
}

func TestSummaryOncePerDay(t *testing.T) {
	ctx := context.Background()
	e, c, _ := statsEnv(t)
	// Вчера (1 января, 12:00) пришёл один оплативший.
	usr, _ := c.UserByTg(ctx, 1001)
	p, _, err := c.CreatePayment(ctx, usr.ID, "manual", 50000)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.ConfirmManual(ctx, owner, p.ID); err != nil {
		t.Fatal(err)
	}
	e.st.DropTasks()

	e.clock.Add(20*time.Hour + 59*time.Minute) // 2 января, 8:59
	if n, err := c.SendSummary(ctx); err != nil || n != 0 || len(summaryTasks(t, e)) != 0 {
		t.Fatalf("до 9:00 итог ушёл: %d %v", n, err)
	}
	e.clock.Add(time.Minute) // 9:00
	if n, err := c.SendSummary(ctx); err != nil || n != 1 {
		t.Fatalf("в 9:00 итога нет: %d %v", n, err)
	}
	got := summaryTasks(t, e)
	if len(got) != 1 || got[0].Vars["revenue"] != "50000" || got[0].Vars["new"] != "1" || got[0].Vars["first_paid"] != "1" || got[0].Vars["date"] != "01.01.2026" {
		t.Fatalf("итог: %+v", got)
	}
	// Тот же день ещё раз и перезапуск процесса в 9:01: второго итога нет.
	e.clock.Add(time.Minute)
	if n, _ := c.SendSummary(ctx); n != 0 {
		t.Errorf("повтор в тот же день: %d", n)
	}
	c2 := core.New(core.Deps{Store: e.st, Clock: e.clock, Config: e.cfg, Panel: e.panel, Notifier: e.notes, OwnerID: owner})
	if n, _ := c2.SendSummary(ctx); n != 0 || len(summaryTasks(t, e)) != 1 {
		t.Errorf("после перезапуска: %d, итогов %d", n, len(summaryTasks(t, e)))
	}
	// Следующий день снова получает свой итог.
	e.clock.Add(24 * time.Hour)
	if n, _ := c.SendSummary(ctx); n != 1 || len(summaryTasks(t, e)) != 2 {
		t.Errorf("следующий день: %d", n)
	}
}

func TestSummaryHourFromSettings(t *testing.T) {
	ctx := context.Background()
	e, c, _ := statsEnv(t)
	e.st.DropTasks()
	if err := e.cfg.Set("summary.hour", "18"); err != nil {
		t.Fatal(err)
	}
	e.clock.Add(24 * time.Hour) // 2 января, 12:00
	if n, _ := c.SendSummary(ctx); n != 0 {
		t.Fatalf("до 18:00 итог ушёл")
	}
	e.clock.Add(6 * time.Hour)
	if n, _ := c.SendSummary(ctx); n != 1 {
		t.Errorf("в 18:00 итога нет")
	}
}

func TestSummaryHourCheck(t *testing.T) {
	ctx := context.Background()
	e := newCfgEnv(t)
	for _, bad := range []string{"24", "-1", "девять"} {
		if _, err := e.c.SetSetting(ctx, owner, "summary.hour", bad); !errors.Is(err, core.ErrBadSetting) {
			t.Errorf("%q: %v", bad, err)
		}
	}
	if _, err := e.c.SetSetting(ctx, owner, "summary.hour", "7"); err != nil || e.cfg.Int("summary.hour") != 7 {
		t.Errorf("7: %v", err)
	}
}

// countStats считает, сколько раз ядро просило у хранилища цифры сводки.
type countStats struct {
	*coretest.Store
	n int
}

func (s *countStats) StatsData(ctx context.Context, q core.StatsQuery) (core.StatsData, error) {
	s.n++
	return s.Store.StatsData(ctx, q)
}

// Итог за день уже отправлен: задача раз в минуту не считает сводку заново.
func TestSummaryDoesNotRecountAfterSent(t *testing.T) {
	ctx := context.Background()
	e := newCfgEnv(t)
	st := &countStats{Store: e.st}
	c := core.New(core.Deps{Store: st, Clock: e.clock, Config: e.cfg, Panel: e.panel, Notifier: e.notes, OwnerID: owner})
	e.clock.Add(20*time.Hour + 59*time.Minute)
	if n, _ := c.SendSummary(ctx); n != 0 || st.n != 0 {
		t.Fatalf("до часа итога: %d, расчётов %d", n, st.n)
	}
	e.clock.Add(time.Minute)
	if n, _ := c.SendSummary(ctx); n != 1 || st.n != 1 {
		t.Fatalf("первый запуск: %d, расчётов %d", n, st.n)
	}
	for range 5 {
		e.clock.Add(time.Minute)
		if n, _ := c.SendSummary(ctx); n != 0 {
			t.Fatal("повторный итог")
		}
	}
	if st.n != 1 {
		t.Errorf("сводка считалась %d раз", st.n)
	}
}

// Дни сводки считаются в часовом поясе настройки summary.tz.
func TestSummaryTimezone(t *testing.T) {
	ctx := context.Background()
	e, c, _ := statsEnv(t)
	e.st.DropTasks()
	if err := e.cfg.Set("summary.tz", "Europe/Moscow"); err != nil {
		t.Fatal(err)
	}
	if got := c.Location().String(); got != "Europe/Moscow" {
		t.Fatalf("пояс %q", got)
	}
	// 2 января 05:59 UTC это 08:59 по Москве: до 9:00 итога нет, через минуту есть.
	e.clock.Add(17*time.Hour + 59*time.Minute)
	if n, _ := c.SendSummary(ctx); n != 0 {
		t.Fatalf("итог до 9:00 по Москве")
	}
	e.clock.Add(time.Minute)
	if n, _ := c.SendSummary(ctx); n != 1 {
		t.Fatalf("итога в 9:00 по Москве нет")
	}
	if _, err := e.c.SetSetting(ctx, owner, "summary.tz", "Нигде/Никак"); !errors.Is(err, core.ErrBadSetting) {
		t.Errorf("неверный пояс: %v", err)
	}
}
