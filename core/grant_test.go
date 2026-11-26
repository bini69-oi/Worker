package core_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/bini69-oi/worker/core"
)

const day = 24 * time.Hour

func TestGrantDays(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	if _, err := e.c.GrantDays(ctx, owner, e.user.ID, 7); !errors.Is(err, core.ErrNoSub) {
		t.Fatalf("без подписки: ждали ErrNoSub, получили %v", err)
	}

	sub := e.bought(t)
	got, err := e.c.GrantDays(ctx, owner, e.user.ID, 7)
	if err != nil {
		t.Fatal(err)
	}
	if !got.ExpiresAt.Equal(sub.ExpiresAt.Add(7 * day)) {
		t.Fatalf("живая продлена не от конца: было %v, стало %v", sub.ExpiresAt, got.ExpiresAt)
	}

	// Истекла: продлевается от сегодня.
	e.clock.Add(60 * day)
	if _, err := e.c.ExpireDue(ctx); err != nil {
		t.Fatal(err)
	}
	got, err = e.c.GrantDays(ctx, owner, e.user.ID, 7)
	if err != nil {
		t.Fatal(err)
	}
	if !got.ExpiresAt.Equal(e.clock.Now().Add(7*day)) || got.Status != core.SubActive {
		t.Fatalf("истёкшая продлена не от сегодня: %v %s", got.ExpiresAt, got.Status)
	}
	if n := countAudit(e, "sub.grant_days"); n != 2 {
		t.Fatalf("записей в журнале %d, ждали 2", n)
	}

	for _, bad := range []int{0, -1, 366} {
		if _, err := e.c.GrantDays(ctx, owner, e.user.ID, bad); !errors.Is(err, core.ErrInvalid) {
			t.Fatalf("%d дней: ждали ErrInvalid, получили %v", bad, err)
		}
	}
}

func TestGrantDaysHelper(t *testing.T) {
	ctx := context.Background()
	e := newSecEnv(t)
	e.bought(t)
	e.addHelperTg(t, 500, core.PermUsers)
	if _, err := e.c.GrantDays(ctx, 500, e.user.ID, 7); !errors.Is(err, core.ErrForbidden) {
		t.Fatalf("без права grant: ждали ErrForbidden, получили %v", err)
	}

	e.addHelperTg(t, 501, core.PermGrant)
	before := len(e.notes.ToAdmins)
	if _, err := e.c.GrantDays(ctx, 501, e.user.ID, 7); err != nil {
		t.Fatal(err)
	}
	e.process(t)
	found := false
	for _, n := range e.notes.ToAdmins[before:] {
		found = found || n.Key == "admin.helper_days"
	}
	if !found {
		t.Fatal("владелец не узнал, что помощник выдал дни")
	}
}
