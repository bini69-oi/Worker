package core_test

import (
	"context"
	"testing"
	"time"
)

func TestUnreachableGetsNoNotices(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	if err := e.c.MarkUnreachable(ctx, e.user.ID); err != nil {
		t.Fatal(err)
	}
	before := len(notifyTasks(t, e))
	e.pay(t, e.user.ID, 500) // оплата кладёт уведомление «пополнено»
	if got := len(notifyTasks(t, e)); got != before {
		t.Fatalf("заблокировавшему бота поставлено уведомление: было %d, стало %d", before, got)
	}

	// Вернулся: уведомления снова ставятся.
	if err := e.c.MarkReachable(ctx, e.user.ID); err != nil {
		t.Fatal(err)
	}
	e.pay(t, e.user.ID, 500)
	if got := len(notifyTasks(t, e)); got == before {
		t.Fatal("после возвращения уведомление не поставлено")
	}
}

func TestMarkUnreachableKeepsFirstTime(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	if err := e.c.MarkUnreachable(ctx, e.user.ID); err != nil {
		t.Fatal(err)
	}
	first, _ := e.st.User(ctx, e.user.ID)
	e.clock.Add(time.Hour)
	if err := e.c.MarkUnreachable(ctx, e.user.ID); err != nil {
		t.Fatal(err)
	}
	again, _ := e.st.User(ctx, e.user.ID)
	if !again.UnreachableAt.Equal(first.UnreachableAt) {
		t.Fatal("повторная отметка сдвинула время")
	}
}
