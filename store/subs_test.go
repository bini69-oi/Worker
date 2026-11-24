package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/bini69-oi/worker/core"
)

func tx(t *testing.T, s *CoreStore, fn func(core.Tx) error) {
	t.Helper()
	if err := s.Tx(context.Background(), fn); err != nil {
		t.Fatal(err)
	}
}

func TestSubOnePerUser(t *testing.T) {
	s, uid := newCoreStore(t)
	trial := core.Subscription{UserID: uid, PeriodDays: 3, Trial: true, Status: core.SubActive,
		ExpiresAt: tNow.Add(72 * time.Hour), CreatedAt: tNow, UpdatedAt: tNow}
	tx(t, s, func(tx core.Tx) error {
		var err error
		trial.ID, err = tx.SaveSub(trial)
		return err
	})
	err := s.Tx(context.Background(), func(tx core.Tx) error {
		_, err := tx.SaveSub(core.Subscription{UserID: uid, Status: core.SubActive, CreatedAt: tNow, UpdatedAt: tNow})
		return err
	})
	if !errors.Is(err, core.ErrDuplicate) {
		t.Fatalf("вторая подписка абонента: ждали ErrDuplicate, получили %v", err)
	}
	got, err := s.SubByUser(context.Background(), uid)
	if err != nil {
		t.Fatal(err)
	}
	if got.TariffID != 0 || !got.Trial || !got.ExpiresAt.Equal(trial.ExpiresAt) {
		t.Fatalf("пробная подписка прочитана не так: %+v", got)
	}
}

func TestActiveSubsUntil(t *testing.T) {
	s, uid := newCoreStore(t)
	sub := core.Subscription{UserID: uid, Status: core.SubActive, ExpiresAt: tNow, CreatedAt: tNow, UpdatedAt: tNow}
	tx(t, s, func(tx core.Tx) error { _, err := tx.SaveSub(sub); return err })
	tx(t, s, func(tx core.Tx) error {
		before, _ := tx.ActiveSubsUntil(tNow.Add(-time.Second))
		at, _ := tx.ActiveSubsUntil(tNow)
		if len(before) != 0 || len(at) != 1 {
			t.Errorf("ждали 0 и 1, получили %d и %d", len(before), len(at))
		}
		return nil
	})
}

func TestNoticeOnce(t *testing.T) {
	s, uid := newCoreStore(t)
	tx(t, s, func(tx core.Tx) error {
		first, err := tx.AddNotice(uid, "remind", "1:2:3", tNow)
		if err != nil {
			return err
		}
		second, err := tx.AddNotice(uid, "remind", "1:2:3", tNow)
		if !first || second {
			t.Errorf("ждали true и false, получили %v и %v", first, second)
		}
		return err
	})
}

func TestPendingBuyReplace(t *testing.T) {
	s, uid := newCoreStore(t)
	var tid int64
	tx(t, s, func(tx core.Tx) error {
		var err error
		tid, err = tx.SaveTariff(core.Tariff{Name: "Т", Reset: "NO_RESET", CreatedAt: tNow, Periods: []core.Period{{Days: 30, Price: 1}, {Days: 90, Price: 2}}})
		return err
	})
	tx(t, s, func(tx core.Tx) error {
		return tx.SetPendingBuy(core.PendingBuy{UserID: uid, TariffID: tid, Days: 30, CreatedAt: tNow})
	})
	tx(t, s, func(tx core.Tx) error {
		return tx.SetPendingBuy(core.PendingBuy{UserID: uid, TariffID: tid, Days: 90, CreatedAt: tNow})
	})
	tx(t, s, func(tx core.Tx) error {
		pb, err := tx.PendingBuy(uid)
		if err == nil && (pb.Days != 90 || !pb.CreatedAt.Equal(tNow)) {
			t.Errorf("выбор не заменился: %+v", pb)
		}
		return err
	})
	tx(t, s, func(tx core.Tx) error { return tx.DeletePendingBuy(uid) })
	err := s.Tx(context.Background(), func(tx core.Tx) error { _, err := tx.PendingBuy(uid); return err })
	if !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("после удаления ждали ErrNotFound, получили %v", err)
	}
}
