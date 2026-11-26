package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/bini69-oi/worker/core"
)

func TestUnreachableAndScreen(t *testing.T) {
	s, id := newCoreStore(t)
	ctx := context.Background()
	err := s.Tx(ctx, func(tx core.Tx) error {
		if err := tx.SetUnreachable(id, tNow); err != nil {
			return err
		}
		return tx.SetScreenMsg(id, 77)
	})
	if err != nil {
		t.Fatal(err)
	}
	u, err := s.User(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if !u.UnreachableAt.Equal(tNow) || u.ScreenMsgID != 77 {
		t.Fatalf("поля не записались: %+v", u)
	}

	// Нулевое время снимает отметку.
	if err := s.Tx(ctx, func(tx core.Tx) error { return tx.SetUnreachable(id, time.Time{}) }); err != nil {
		t.Fatal(err)
	}
	if u, _ = s.User(ctx, id); !u.UnreachableAt.IsZero() {
		t.Fatalf("отметка не снята: %v", u.UnreachableAt)
	}

	if err := s.Tx(ctx, func(tx core.Tx) error { return tx.SetScreenMsg(999, 1) }); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("ждали ErrNotFound, получили %v", err)
	}
}
