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

func TestSetBanned(t *testing.T) {
	s, id := newCoreStore(t)
	ctx := context.Background()
	if err := s.Tx(ctx, func(tx core.Tx) error { return tx.SetBanned(id, tNow) }); err != nil {
		t.Fatal(err)
	}
	if u, _ := s.User(ctx, id); !u.BannedAt.Equal(tNow) {
		t.Fatalf("бан не записан: %v", u.BannedAt)
	}
	if err := s.Tx(ctx, func(tx core.Tx) error { return tx.SetBanned(id, time.Time{}) }); err != nil {
		t.Fatal(err)
	}
	if u, _ := s.User(ctx, id); !u.BannedAt.IsZero() {
		t.Fatal("бан не снят")
	}
}

func TestSupportMsgs(t *testing.T) {
	s, id := newCoreStore(t)
	ctx := context.Background()
	if err := s.Tx(ctx, func(tx core.Tx) error { return tx.AddSupportMsg(77, id, tNow) }); err != nil {
		t.Fatal(err)
	}
	err := s.Tx(ctx, func(tx core.Tx) error {
		uid, at, err := tx.SupportMsg(77)
		if err != nil || uid != id || !at.Equal(tNow) {
			t.Fatalf("связь: %d %v %v", uid, at, err)
		}
		_, _, err = tx.SupportMsg(78)
		if !errors.Is(err, core.ErrNotFound) {
			t.Fatalf("нет связи: %v", err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if n, err := s.DeleteSupportBefore(ctx, tNow.Add(time.Second)); err != nil || n != 1 {
		t.Fatalf("очистка: %d %v", n, err)
	}
}

// TestNoticeWithoutUser проверяет общие отметки (user_id 0): по ним тревоги админам уходят раз в окно.
func TestNoticeWithoutUser(t *testing.T) {
	s, _ := newCoreStore(t)
	for i, want := range []bool{true, false} {
		var got bool
		err := s.Tx(context.Background(), func(tx core.Tx) error {
			var err error
			got, err = tx.AddNotice(0, "channel_check", "2026-01-01T12", tNow)
			return err
		})
		if err != nil || got != want {
			t.Fatalf("попытка %d: %v %v, ждали %v", i+1, got, err, want)
		}
	}
}
