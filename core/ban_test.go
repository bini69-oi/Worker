package core_test

import (
	"context"
	"errors"
	"testing"

	"github.com/bini69-oi/worker/core"
)

func TestBan(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	e.bought(t)
	tr, _ := e.c.Tariffs(ctx)

	if err := e.c.Ban(ctx, owner, e.user.ID, "спам"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.c.Buy(ctx, e.user.ID, tr[0].ID, 30); !errors.Is(err, core.ErrBanned) {
		t.Fatalf("забаненный купил: %v", err)
	}
	e.process(t)
	if got, _ := e.panel.User("u1001"); got.Enabled {
		t.Fatal("панель не выключила забаненного")
	}
	if n := countAudit(e, "user.ban"); n != 1 {
		t.Fatalf("записей бана в журнале %d, ждали 1", n)
	}

	// Повторный бан ничего не пишет.
	if err := e.c.Ban(ctx, owner, e.user.ID, ""); err != nil {
		t.Fatal(err)
	}
	if n := countAudit(e, "user.ban"); n != 1 {
		t.Fatalf("повторный бан записан: %d", n)
	}

	if err := e.c.Unban(ctx, owner, e.user.ID, ""); err != nil {
		t.Fatal(err)
	}
	e.process(t)
	if got, _ := e.panel.User("u1001"); !got.Enabled {
		t.Fatal("панель не включила разбаненного")
	}
}

func TestBanNeedsPerm(t *testing.T) {
	ctx := context.Background()
	e := newSecEnv(t)
	e.addHelperTg(t, 500, core.PermUsers, core.PermGrant)
	if err := e.c.Ban(ctx, 500, e.user.ID, ""); !errors.Is(err, core.ErrForbidden) {
		t.Fatalf("ждали ErrForbidden, получили %v", err)
	}
	if u, _ := e.st.User(ctx, e.user.ID); !u.BannedAt.IsZero() {
		t.Fatal("бан без права прошёл")
	}

	e.addHelperTg(t, 501, core.PermBan)
	if err := e.c.Ban(ctx, 501, e.user.ID, ""); err != nil {
		t.Fatalf("помощник с правом ban: %v", err)
	}
}

func countAudit(e *env, action string) int {
	n := 0
	for _, a := range e.st.Audit() {
		if a.Action == action {
			n++
		}
	}
	return n
}
