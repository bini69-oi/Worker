package core_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/bini69-oi/worker/core"
	"github.com/bini69-oi/worker/core/coretest"
)

var t0 = time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

func newCore(t *testing.T) (*core.Core, *coretest.Store, core.User) {
	t.Helper()
	st := coretest.NewStore()
	c := core.New(core.Deps{Store: st, Clock: coretest.NewClock(t0), Config: coretest.NewConfig(t), OwnerID: owner})
	u, err := c.Register(context.Background(), 1001, "ivan", "ru")
	if err != nil {
		t.Fatal(err)
	}
	return c, st, u
}

// moveTx проводит одно движение денег в своей транзакции.
func moveTx(st *coretest.Store, e core.LedgerEntry) error {
	return st.Tx(context.Background(), func(tx core.Tx) error {
		_, err := core.Move(tx, t0, e)
		return err
	})
}

func balance(t *testing.T, st *coretest.Store, id int64) int64 {
	t.Helper()
	u, err := st.User(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return u.Balance
}

func TestHistoryMatchesBalance(t *testing.T) {
	c, st, u := newCore(t)
	if err := moveTx(st, core.LedgerEntry{UserID: u.ID, Amount: 50000, Reason: core.ReasonTopup, RefType: "payment", RefID: 1}); err != nil {
		t.Fatal(err)
	}
	if err := moveTx(st, core.LedgerEntry{UserID: u.ID, Amount: -15000, Reason: core.ReasonBuy}); err != nil {
		t.Fatal(err)
	}
	hist, err := c.History(context.Background(), u.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(hist) != 2 || hist[0].Amount != -15000 || hist[1].Amount != 50000 {
		t.Fatalf("история не та: %+v", hist)
	}
	if b := balance(t, st, u.ID); b != 35000 {
		t.Fatalf("баланс %d, ждали 35000", b)
	}
}

func TestNoNegativeBalance(t *testing.T) {
	_, st, u := newCore(t)
	if err := moveTx(st, core.LedgerEntry{UserID: u.ID, Amount: 1000, Reason: core.ReasonTopup}); err != nil {
		t.Fatal(err)
	}
	err := moveTx(st, core.LedgerEntry{UserID: u.ID, Amount: -1500, Reason: core.ReasonBuy})
	if !errors.Is(err, core.ErrNotEnoughMoney) {
		t.Fatalf("ждали ErrNotEnoughMoney, получили %v", err)
	}
	if b := balance(t, st, u.ID); b != 1000 {
		t.Fatalf("баланс %d, ждали 1000", b)
	}
}

func TestKopecksAddUp(t *testing.T) {
	_, st, u := newCore(t)
	for range 3 {
		if err := moveTx(st, core.LedgerEntry{UserID: u.ID, Amount: 3333, Reason: core.ReasonPromo}); err != nil {
			t.Fatal(err)
		}
	}
	if b := balance(t, st, u.ID); b != 9999 {
		t.Fatalf("баланс %d, ждали 9999", b)
	}
}

func TestSameEventMovesOnce(t *testing.T) {
	_, st, u := newCore(t)
	e := core.LedgerEntry{UserID: u.ID, Amount: 30000, Reason: core.ReasonTopup, RefType: "payment", RefID: 7}
	if err := moveTx(st, e); err != nil {
		t.Fatal(err)
	}
	if err := moveTx(st, e); !errors.Is(err, core.ErrDuplicate) {
		t.Fatalf("ждали ErrDuplicate, получили %v", err)
	}
	if b := balance(t, st, u.ID); b != 30000 {
		t.Fatalf("баланс %d, ждали 30000", b)
	}
}

func TestZeroAmountRejected(t *testing.T) {
	_, st, u := newCore(t)
	if err := moveTx(st, core.LedgerEntry{UserID: u.ID, Reason: core.ReasonTopup}); !errors.Is(err, core.ErrZeroAmount) {
		t.Fatalf("ждали ErrZeroAmount, получили %v", err)
	}
}

func TestRegisterOnce(t *testing.T) {
	c, _, u := newCore(t)
	again, err := c.Register(context.Background(), 1001, "other", "en")
	if err != nil {
		t.Fatal(err)
	}
	if again.ID != u.ID {
		t.Fatalf("повторная регистрация создала второго абонента: %d и %d", u.ID, again.ID)
	}
}
