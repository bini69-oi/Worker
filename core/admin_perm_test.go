package core_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/bini69-oi/worker/core"
	"github.com/bini69-oi/worker/core/coretest"
)

// helperWith заводит помощника с правами через «Да» владельца.
func helperWith(t *testing.T, e *env, tg int64, perms ...core.Perm) {
	t.Helper()
	ctx := context.Background()
	id, err := e.c.RequestAddHelper(ctx, owner, tg, perms, "helper password 1")
	if err != nil {
		t.Fatal(err)
	}
	if err := e.c.AnswerConfirm(ctx, id, owner, true); err != nil {
		t.Fatal(err)
	}
}

func TestRequire(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	helperWith(t, e, 50, core.PermUsers)
	cases := []struct {
		name string
		tg   int64
		perm core.Perm
		want error
	}{
		{"владелец, любое право", owner, core.PermTariffs, nil},
		{"владелец, только владельцу", owner, core.PermOwner, nil},
		{"помощник с правом", 50, core.PermUsers, nil},
		{"помощник без права", 50, core.PermTariffs, core.ErrForbidden},
		{"помощник, только владельцу", 50, core.PermOwner, core.ErrOwnerOnly},
		{"любой админ", 50, "", nil},
		{"не админ", 777, "", core.ErrForbidden},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			before := len(e.st.Audit())
			a, err := e.c.Require(ctx, c.tg, c.perm, "test")
			if !errors.Is(err, c.want) || (c.want == nil && a.TgID != c.tg) {
				t.Fatalf("ошибка %v, ждали %v", err, c.want)
			}
			denied := len(e.st.Audit()) - before
			if (c.want != nil) != (denied == 1) {
				t.Errorf("записей об отказе %d при ошибке %v", denied, c.want)
			}
		})
	}
	// Отказ приходит владельцу тревогой.
	found := false
	for _, n := range adminTasks(t, e) {
		found = found || n.Key == "admin.denied"
	}
	if !found {
		t.Errorf("тревоги об отказе нет")
	}
}

// Журнал пишет каждый отказ, а тревога по паре «админ и действие» уходит раз в час.
func TestDeniedAlertOncePerHour(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	helperWith(t, e, 50, core.PermUsers)
	alerts := func() int {
		n := 0
		for _, a := range adminTasks(t, e) {
			if a.Key == "admin.denied" {
				n++
			}
		}
		return n
	}
	for range 5 {
		if _, err := e.c.Require(ctx, 50, core.PermTariffs, "tariff.save"); !errors.Is(err, core.ErrForbidden) {
			t.Fatal(err)
		}
	}
	denied := 0
	for _, a := range e.st.Audit() {
		if a.Action == "denied:tariff.save" {
			denied++
		}
	}
	if denied != 5 || alerts() != 1 {
		t.Fatalf("записей %d, задач тревоги %d", denied, alerts())
	}
	if _, err := e.c.Require(ctx, 50, core.PermPromo, "promo.save"); err == nil {
		t.Fatal()
	}
	if alerts() != 2 {
		t.Errorf("другое действие должно дать свою тревогу: %d", alerts())
	}
	e.clock.Add(time.Hour)
	_, _ = e.c.Require(ctx, 50, core.PermTariffs, "tariff.save")
	if alerts() != 3 {
		t.Errorf("через час тревога снова: %d", alerts())
	}
}

// writeCounter считает транзакции записи: обычный запрос админки идёт чтением.
type writeCounter struct {
	*coretest.Store
	n int
}

func (s *writeCounter) Tx(ctx context.Context, fn func(core.Tx) error) error {
	s.n++
	return s.Store.Tx(ctx, fn)
}

func TestAdminRequestIsReadOnly(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	st := &writeCounter{Store: e.st}
	c := core.New(core.Deps{Store: st, Clock: e.clock, Config: e.cfg, Panel: e.panel, Notifier: e.notes, OwnerID: owner})
	if err := c.SetOwnerPassword(ctx, ownerPass); err != nil {
		t.Fatal(err)
	}
	info := core.LoginInfo{IP: "203.0.113.5", Agent: "test"}
	step, err := c.LoginStart(ctx, owner, ownerPass, info)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.AnswerLogin(ctx, step.ConfirmID, owner, step.Code); err != nil {
		t.Fatal(err)
	}
	token, _, err := c.LoginFinish(ctx, step.ConfirmID, step.Nonce, info)
	if err != nil {
		t.Fatal(err)
	}
	st.n = 0
	for range 5 {
		a, _, err := c.SessionAdmin(ctx, token, info)
		if err != nil || !a.Owner {
			t.Fatal(err)
		}
		if _, err := c.Require(ctx, owner, core.PermStats, "stats.get"); err != nil {
			t.Fatal(err)
		}
		if _, err := c.Stats(ctx, owner, janDay(1), janDay(2)); err != nil {
			t.Fatal(err)
		}
	}
	if st.n != 0 {
		t.Errorf("запросы админки открыли %d транзакций записи", st.n)
	}
	// Отказ пишется отдельной записью.
	helperWith(t, e, 50, core.PermUsers)
	st.n = 0
	_, _ = c.Require(ctx, 50, core.PermTariffs, "tariff.save")
	if st.n != 1 {
		t.Errorf("отказ должен писаться одной записью: %d", st.n)
	}
	// Сессия, которую не видели больше минуты, обновляется записью.
	e.clock.Add(2 * time.Minute)
	st.n = 0
	if _, _, err := c.SessionAdmin(ctx, token, info); err != nil || st.n != 1 {
		t.Errorf("обновление времени сессии: %v, записей %d", err, st.n)
	}
}
