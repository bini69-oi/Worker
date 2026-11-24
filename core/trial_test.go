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
