package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/bini69-oi/worker/core"
)

func TestSegments(t *testing.T) {
	s, uid := newCoreStore(t) // ivan без подписки
	ids := seedUsers(t, s)
	tx(t, s, func(tx core.Tx) error { return tx.SetUnreachable(ids["alice"], tNow) }) // alice заблокировала бота
	ctx := context.Background()
	cases := map[string][]string{
		core.SegAll:       {"ivan", "a%_x", "abcde", "old", "ancient"}, // без bad (бан) и alice (недоступна)
		core.SegActive:    {"abcde"},
		core.SegTrial:     {"abcde"},
		core.SegNone:      {"ivan", "a%_x"},
		core.SegExpired30: {"old"},
	}
	_ = uid
	for seg, want := range cases {
		n, err := s.SegmentCount(ctx, seg, tNow)
		if err != nil || n != len(want) {
			t.Errorf("%s: счётчик %d, ждали %d (%v)", seg, n, len(want), err)
		}
		users, err := s.SegmentUsers(ctx, seg, 0, tNow, 50)
		if err != nil || len(users) != len(want) {
			t.Fatalf("%s: %v %v", seg, users, err)
		}
		for i := 1; i < len(users); i++ {
			if users[i-1].ID >= users[i].ID {
				t.Errorf("%s: получатели не по возрастанию id", seg)
			}
		}
	}
	all, _ := s.SegmentUsers(ctx, core.SegAll, 0, tNow, 2)
	rest, _ := s.SegmentUsers(ctx, core.SegAll, all[1].ID, tNow, 50)
	if len(all) != 2 || len(rest) != 3 {
		t.Errorf("курсор: %d и %d", len(all), len(rest))
	}
}

func TestBroadcastLifecycle(t *testing.T) {
	s, _ := newCoreStore(t)
	ctx := context.Background()
	var a, b int64
	tx(t, s, func(tx core.Tx) error {
		var err error
		if a, err = tx.CreateBroadcast(core.Broadcast{Segment: core.SegAll, Text: "a", Status: core.BcDraft, CreatedBy: 1, CreatedAt: tNow}); err != nil {
			return err
		}
		b, err = tx.CreateBroadcast(core.Broadcast{Segment: core.SegAll, Text: "b", Status: core.BcDraft, CreatedBy: 1, CreatedAt: tNow})
		return err
	})
	tx(t, s, func(tx core.Tx) error {
		if ok, err := tx.StartBroadcast(a, 10, tNow); err != nil || !ok {
			t.Fatalf("старт: %v %v", ok, err)
		}
		if _, err := tx.StartBroadcast(b, 10, tNow); !errors.Is(err, core.ErrDuplicate) {
			t.Errorf("вторая идущая: %v", err)
		}
		return nil
	})
	tx(t, s, func(tx core.Tx) error {
		for i := int64(1); i <= 3; i++ {
			sent, blocked := 1, 0
			if i == 2 {
				sent, blocked = 0, 1
			}
			if ok, err := tx.BroadcastStep(a, i, sent, 0, blocked); err != nil || !ok {
				t.Fatalf("шаг %d: %v %v", i, ok, err)
			}
		}
		return nil
	})
	got, err := s.BroadcastByID(ctx, a)
	if err != nil || got.Sent != 2 || got.Blocked != 1 || got.CursorUserID != 3 || got.Total != 10 || !got.StartedAt.Equal(tNow) {
		t.Fatalf("%+v %v", got, err)
	}
	tx(t, s, func(tx core.Tx) error {
		if ok, _ := tx.CancelBroadcast(a, tNow.Add(time.Minute)); !ok {
			t.Error("отмена идущей")
		}
		if ok, _ := tx.BroadcastStep(a, 4, 1, 0, 0); ok {
			t.Error("шаг после отмены считает рассылку идущей")
		}
		if ok, _ := tx.FinishBroadcast(a, tNow); ok {
			t.Error("отменённая закончена")
		}
		return nil
	})
	if got, _ = s.BroadcastByID(ctx, a); got.Status != core.BcCancelled || got.Sent != 3 { // сообщение, ушедшее при отмене, в счётчиках
		t.Errorf("после отмены: %+v", got)
	}
	list, _ := s.Broadcasts(ctx, 0, 10)
	if len(list) != 2 || list[0].ID != b {
		t.Errorf("список: %+v", list)
	}
}
