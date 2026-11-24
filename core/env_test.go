package core_test

import (
	"context"
	"testing"

	"github.com/bini69-oi/worker/core"
	"github.com/bini69-oi/worker/core/coretest"
	"github.com/bini69-oi/worker/settings"
)

type env struct {
	c     *core.Core
	st    *coretest.Store
	clock *coretest.Clock
	cfg   *settings.Settings
	user  core.User
}

func newEnv(t *testing.T) *env {
	t.Helper()
	e := &env{st: coretest.NewStore(), clock: coretest.NewClock(t0), cfg: coretest.NewConfig(t)}
	e.c = core.New(core.Deps{Store: e.st, Clock: e.clock, Config: e.cfg})
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
	tr, err := e.c.SaveTariff(context.Background(), core.Tariff{Name: name, Active: true, Periods: periods, Squads: []string{"a", "b"}})
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
