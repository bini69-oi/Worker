package core_test

import (
	"context"
	"testing"

	"github.com/bini69-oi/worker/core"
	"github.com/bini69-oi/worker/core/coretest"
	"github.com/bini69-oi/worker/settings"
)

// owner это Telegram ID владельца в тестах ядра.
const owner = 1

type env struct {
	c     *core.Core
	st    *coretest.Store
	clock *coretest.Clock
	cfg   *settings.Settings
	panel *coretest.Panel
	notes *coretest.Notifier
	user  core.User
}

func newEnv(t *testing.T) *env {
	t.Helper()
	e := &env{st: coretest.NewStore(), clock: coretest.NewClock(t0), cfg: coretest.NewConfig(t),
		panel: coretest.NewPanel(), notes: &coretest.Notifier{}}
	e.c = core.New(core.Deps{Store: e.st, Clock: e.clock, Config: e.cfg, Panel: e.panel, Notifier: e.notes, OwnerID: owner})
	u, err := e.c.Register(context.Background(), 1001, "ivan", "ru")
	if err != nil {
		t.Fatal(err)
	}
	e.user = u
	return e
}

func (e *env) topup(t *testing.T, amount int64) {
	t.Helper()
	if _, err := e.c.AdminAdjust(context.Background(), 1, e.user.ID, amount, "тест"); err != nil {
		t.Fatal(err)
	}
}

func (e *env) tariff(t *testing.T, name string, periods ...core.Period) core.Tariff {
	t.Helper()
	tr, err := e.c.SaveTariff(context.Background(), owner, core.Tariff{Name: name, Active: true, Periods: periods, Squads: []string{"a", "b"}})
	if err != nil {
		t.Fatal(err)
	}
	return tr
}

func (e *env) syncTasks() int {
	n := 0
	for _, task := range e.st.Tasks() {
		if task.Kind == core.KindPanelSync {
			n++
		}
	}
	return n
}

// process выполняет очередь, как воркер.
func (e *env) process(t *testing.T) {
	t.Helper()
	if _, err := e.c.ProcessDue(context.Background()); err != nil {
		t.Fatal(err)
	}
}

// bought покупает тариф на 30 дней и прогоняет очередь, чтобы абонент появился в панели.
func (e *env) bought(t *testing.T) core.Subscription {
	t.Helper()
	tr := e.tariff(t, "Месяц", core.Period{Days: 30, Price: 100})
	e.topup(t, 10000)
	if _, err := e.c.Buy(context.Background(), e.user.ID, tr.ID, 30); err != nil {
		t.Fatal(err)
	}
	e.process(t)
	sub, err := e.c.Sub(context.Background(), e.user.ID)
	if err != nil {
		t.Fatal(err)
	}
	return sub
}

func (e *env) balance(t *testing.T) int64 {
	t.Helper()
	u, err := e.st.User(context.Background(), e.user.ID)
	if err != nil {
		t.Fatal(err)
	}
	return u.Balance
}
