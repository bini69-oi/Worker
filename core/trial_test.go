package core_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/bini69-oi/worker/core"
)

func TestTrialOnce(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	sub, err := e.c.Trial(ctx, e.user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !sub.Trial || !sub.ExpiresAt.Equal(t0.Add(3*24*time.Hour)) || e.syncTasks() != 1 {
		t.Fatalf("пробный период не тот: %+v, задач %d", sub, e.syncTasks())
	}
	if _, err := e.c.Trial(ctx, e.user.ID); !errors.Is(err, core.ErrTrialUsed) {
		t.Fatalf("повторный пробный: ждали ErrTrialUsed, получили %v", err)
	}
}

func TestTrialOff(t *testing.T) {
	e := newEnv(t)
	if err := e.cfg.Set("trial.enabled", "false"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.c.Trial(context.Background(), e.user.ID); !errors.Is(err, core.ErrTrialOff) {
		t.Fatalf("ждали ErrTrialOff, получили %v", err)
	}
}

func TestTrialHourlyLimit(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	if err := e.cfg.Set("trial.max_per_hour", "3"); err != nil {
		t.Fatal(err)
	}
	for i := range 5 {
		u, err := e.c.Register(ctx, int64(7000+i), "new", "ru")
		if err != nil {
			t.Fatal(err)
		}
		_, err = e.c.Trial(ctx, u.ID)
		if i < 3 && err != nil {
			t.Fatalf("триал %d: %v", i+1, err)
		}
		if i >= 3 && !errors.Is(err, core.ErrTrialBusy) {
			t.Fatalf("триал сверх предела: %v", err)
		}
	}
	e.process(t)
	n := 0
	for _, a := range e.notes.ToAdmins {
		if a.Key == "admin.trial_limit" {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("тревог о пределе триалов %d, ждали 1", n)
	}
	e.clock.Add(time.Hour)
	u, _ := e.c.Register(ctx, 7999, "later", "ru")
	if _, err := e.c.Trial(ctx, u.ID); err != nil {
		t.Fatalf("через час триал должен выдаваться: %v", err)
	}
}
