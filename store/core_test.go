package store

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/bini69-oi/worker/core"
)

var tNow = time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

func newCoreStore(t *testing.T) (*CoreStore, int64) {
	t.Helper()
	s := NewCoreStore(openTest(t))
	var id int64
	err := s.Tx(context.Background(), func(tx core.Tx) error {
		var err error
		id, err = tx.CreateUser(core.User{TgID: 1001, Username: "ivan", Lang: "ru", CreatedAt: tNow})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return s, id
}

func add(s *CoreStore, e core.LedgerEntry) error {
	return s.Tx(context.Background(), func(tx core.Tx) error {
		e.CreatedAt = tNow
		_, err := tx.AddLedger(e)
		return err
	})
}

func TestUserRoundTrip(t *testing.T) {
	s, id := newCoreStore(t)
	u, err := s.UserByTg(context.Background(), 1001)
	if err != nil {
		t.Fatal(err)
	}
	if u.ID != id || u.Username != "ivan" || u.Lang != "ru" || !u.CreatedAt.Equal(tNow) || !u.BannedAt.IsZero() {
		t.Fatalf("абонент прочитан не так: %+v", u)
	}
	if _, err := s.User(context.Background(), 999); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("ждали ErrNotFound, получили %v", err)
	}
}

func TestDuplicateTgID(t *testing.T) {
	s, _ := newCoreStore(t)
	err := s.Tx(context.Background(), func(tx core.Tx) error {
		_, err := tx.CreateUser(core.User{TgID: 1001, CreatedAt: tNow})
		return err
	})
	if !errors.Is(err, core.ErrDuplicate) {
		t.Fatalf("ждали ErrDuplicate, получили %v", err)
	}
}

func TestLedgerSumEqualsBalance(t *testing.T) {
	s, id := newCoreStore(t)
	for _, amount := range []int64{50000, -15000, 3333, 3333, 3333} {
		if err := add(s, core.LedgerEntry{UserID: id, Amount: amount, Reason: core.ReasonTopup}); err != nil {
			t.Fatal(err)
		}
	}
	var sum int64
	if err := s.db.R.QueryRow("SELECT SUM(amount) FROM ledger WHERE user_id = ?", id).Scan(&sum); err != nil {
		t.Fatal(err)
	}
	u, err := s.User(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if sum != 44999 || u.Balance != sum {
		t.Fatalf("сумма истории %d, баланс %d, ждали 44999", sum, u.Balance)
	}
}

func TestLedgerDuplicateRef(t *testing.T) {
	s, id := newCoreStore(t)
	e := core.LedgerEntry{UserID: id, Amount: 30000, Reason: core.ReasonTopup, RefType: "payment", RefID: 7}
	if err := add(s, e); err != nil {
		t.Fatal(err)
	}
	if err := add(s, e); !errors.Is(err, core.ErrDuplicate) {
		t.Fatalf("ждали ErrDuplicate, получили %v", err)
	}
	if u, _ := s.User(context.Background(), id); u.Balance != 30000 {
		t.Fatalf("баланс %d, ждали 30000", u.Balance)
	}
}

func TestLedgerNegativeRollsBack(t *testing.T) {
	s, id := newCoreStore(t)
	if err := add(s, core.LedgerEntry{UserID: id, Amount: 1000, Reason: core.ReasonTopup}); err != nil {
		t.Fatal(err)
	}
	if err := add(s, core.LedgerEntry{UserID: id, Amount: -1500, Reason: core.ReasonBuy}); !errors.Is(err, core.ErrNotEnoughMoney) {
		t.Fatalf("ждали ErrNotEnoughMoney, получили %v", err)
	}
	var n int
	if err := s.db.R.QueryRow("SELECT COUNT(*) FROM ledger").Scan(&n); err != nil || n != 1 {
		t.Fatalf("строка списания осталась в истории: %d (%v)", n, err)
	}
}

func TestLedgerParallelSameRef(t *testing.T) {
	s, id := newCoreStore(t)
	e := core.LedgerEntry{UserID: id, Amount: 30000, Reason: core.ReasonTopup, RefType: "payment", RefID: 9}
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() { _ = add(s, e) })
	}
	wg.Wait()
	if u, _ := s.User(context.Background(), id); u.Balance != 30000 {
		t.Fatalf("20 одновременных зачислений одного платежа дали баланс %d, ждали 30000", u.Balance)
	}
}

func TestLedgerOrderAndFields(t *testing.T) {
	s, id := newCoreStore(t)
	if err := add(s, core.LedgerEntry{UserID: id, Amount: 10000, Reason: core.ReasonAdmin, AdminID: 42, Comment: "компенсация"}); err != nil {
		t.Fatal(err)
	}
	if err := add(s, core.LedgerEntry{UserID: id, Amount: -500, Reason: core.ReasonBuy}); err != nil {
		t.Fatal(err)
	}
	hist, err := s.Ledger(context.Background(), id, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(hist) != 2 || hist[0].Amount != -500 || hist[1].AdminID != 42 || hist[1].Comment != "компенсация" {
		t.Fatalf("история прочитана не так: %+v", hist)
	}
}
