package core_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/bini69-oi/worker/core"
)

// Помощник нажал «Импорт»: отказ, подтверждения владельцу нет, абонентов нет.
func TestImportButtonHelperDenied(t *testing.T) {
	ctx := context.Background()
	e := newCfgEnv(t)
	e.listed("r1", "old_one", 2001, 0)
	helperWith(t, e.env, 50, core.PermUsers, core.PermGrant, core.PermStats, core.PermBan, core.PermPromo, core.PermTariffs, core.PermBroadcast, core.PermSupport)
	confirms := func() int {
		n := 0
		for _, task := range e.st.Tasks() {
			if task.Kind == core.KindNotifyConfirm {
				n++
			}
		}
		return n
	}
	before := confirms()
	if _, err := e.c.RequestImport(ctx, 50); !errors.Is(err, core.ErrOwnerOnly) {
		t.Fatalf("помощник: %v", err)
	}
	if _, err := e.c.ImportPanel(ctx, 50, false); !errors.Is(err, core.ErrOwnerOnly) {
		t.Errorf("пробный прогон помощника: %v", err)
	}
	if confirms() != before {
		t.Errorf("владельцу ушёл запрос на «Да» от помощника")
	}
	if _, err := e.c.UserByTg(ctx, 2001); !errors.Is(err, core.ErrNotFound) {
		t.Errorf("абонент заведён: %v", err)
	}
}

func TestImportButtonAfterYes(t *testing.T) {
	ctx := context.Background()
	e := newCfgEnv(t)
	e.listed("r1", "old_one", 2001, 0)
	id, err := e.c.RequestImport(ctx, owner)
	if err != nil || id == 0 {
		t.Fatal(id, err)
	}
	if _, err := e.c.UserByTg(ctx, 2001); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("абонент заведён до «Да»: %v", err)
	}
	confirmLast(t, e, true)
	if _, err := e.c.UserByTg(ctx, 2001); err != nil {
		t.Fatalf("после «Да» абонента нет: %v", err)
	}
	done := false
	for _, a := range e.st.Audit() {
		done = done || (a.Action == "import.panel" && a.Details["subs"] == "1")
	}
	if !done {
		t.Errorf("итог импорта не записан в журнал")
	}
}

func TestImportButtonRejected(t *testing.T) {
	ctx := context.Background()
	e := newCfgEnv(t)
	e.listed("r1", "old_one", 2001, 0)
	if _, err := e.c.RequestImport(ctx, owner); err != nil {
		t.Fatal(err)
	}
	confirmLast(t, e, false)
	if _, err := e.c.UserByTg(ctx, 2001); !errors.Is(err, core.ErrNotFound) {
		t.Errorf("после «Нет» абонент заведён: %v", err)
	}
}

// Пробный прогон читает всю панель: не чаще раза в минуту.
func TestPreviewImportRateLimited(t *testing.T) {
	ctx := context.Background()
	e := newCfgEnv(t)
	e.listed("r1", "old_one", 2001, 0)
	if _, err := e.c.PreviewImport(ctx, owner); err != nil {
		t.Fatal(err)
	}
	if _, err := e.c.PreviewImport(ctx, owner); !errors.Is(err, core.ErrTooManyTries) {
		t.Fatalf("второй прогон сразу: %v", err)
	}
	e.clock.Add(time.Minute)
	if _, err := e.c.PreviewImport(ctx, owner); err != nil {
		t.Errorf("через минуту: %v", err)
	}
	helperWith(t, e.env, 50, core.PermUsers)
	if _, err := e.c.PreviewImport(ctx, 50); !errors.Is(err, core.ErrOwnerOnly) {
		t.Errorf("помощник: %v", err)
	}
}
