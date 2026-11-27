package core_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/bini69-oi/worker/core"
)

func TestSetMaintenance(t *testing.T) {
	ctx := context.Background()
	e := newCfgEnv(t)
	if err := e.c.SetMaintenance(ctx, owner, true, "Обновляем серверы, вернёмся в 15:00"); err != nil {
		t.Fatal(err)
	}
	if !e.c.Maintenance() {
		t.Fatal("техработы не включились")
	}
	for _, key := range []string{"bot.maintenance", "err.maintenance"} {
		if got := e.tx.Raw(key, "ru"); got != "Обновляем серверы, вернёмся в 15:00" {
			t.Errorf("%s: абонент видит %q", key, got)
		}
	}
	// Абонент во время техработ получает отказ, админ нет.
	if err := e.c.Serving(ctx, e.user.ID); !errors.Is(err, core.ErrMaintenance) {
		t.Errorf("абонент: %v", err)
	}
	st, err := e.c.MaintenanceState(ctx, owner)
	if err != nil || !st.On || !strings.Contains(st.Text, "15:00") {
		t.Errorf("состояние: %+v %v", st, err)
	}

	if err := e.c.SetMaintenance(ctx, owner, false, ""); err != nil {
		t.Fatal(err)
	}
	if e.c.Maintenance() {
		t.Error("техработы не выключились")
	}
	var actions []string
	for _, a := range e.st.Audit() {
		if strings.HasPrefix(a.Action, "maintenance.") {
			actions = append(actions, a.Action)
		}
	}
	if len(actions) != 2 || actions[0] != "maintenance.on" || actions[1] != "maintenance.off" {
		t.Errorf("журнал: %v", actions)
	}
}

func TestSetMaintenanceRefusals(t *testing.T) {
	ctx := context.Background()
	e := newCfgEnv(t)
	helperWith(t, e.env, 50, core.PermUsers, core.PermSupport)
	if err := e.c.SetMaintenance(ctx, 50, true, "x"); !errors.Is(err, core.ErrOwnerOnly) || e.c.Maintenance() {
		t.Errorf("помощник: %v", err)
	}
	if err := e.c.SetMaintenance(ctx, owner, true, strings.Repeat("я", 4097)); !errors.Is(err, core.ErrTooLong) || e.c.Maintenance() {
		t.Errorf("длинный текст: %v", err)
	}
	if err := e.c.SetMaintenance(ctx, owner, true, "ещё {days}"); !errors.Is(err, core.ErrBadText) || e.c.Maintenance() {
		t.Errorf("подстановка в тексте: %v", err)
	}
	// Включение без текста оставляет прежний текст.
	before := e.tx.Raw("bot.maintenance", "ru")
	if err := e.c.SetMaintenance(ctx, owner, true, ""); err != nil || e.tx.Raw("bot.maintenance", "ru") != before {
		t.Errorf("без текста: %v", err)
	}
}
