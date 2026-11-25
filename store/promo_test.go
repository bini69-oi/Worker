package store

import (
	"context"
	"sync"
	"testing"

	"github.com/bini69-oi/worker/core"
	"github.com/bini69-oi/worker/core/coretest"
)

func sqlCore(t *testing.T) (*core.Core, *CoreStore) {
	t.Helper()
	s := NewCoreStore(openTest(t))
	return core.New(core.Deps{Store: s, Clock: coretest.NewClock(tNow), Config: coretest.NewConfig(t), OwnerID: 1}), s
}

// TestPromoLimitConcurrent: 10 абонентов одновременно вводят код на 3 активации, проходят ровно 3.
func TestPromoLimitConcurrent(t *testing.T) {
	c, s := sqlCore(t)
	ctx := context.Background()
	if _, err := c.SavePromo(ctx, 1, core.Promo{Code: "Three3", Kind: core.PromoMoney, Value: 100, MaxUses: 3, Active: true}); err != nil {
		t.Fatal(err)
	}
	var users []core.User
	for i := range 10 {
		u, err := c.Register(ctx, int64(100+i), "u", "ru")
		if err != nil {
			t.Fatal(err)
		}
		users = append(users, u)
	}
	var wg sync.WaitGroup
	for _, u := range users {
		wg.Go(func() { _, _ = c.ApplyPromo(ctx, u.ID, " three3 ") })
	}
	wg.Wait()
	var uses, rows int
	if err := s.db.R.QueryRow("SELECT uses FROM promo_codes").Scan(&uses); err != nil {
		t.Fatal(err)
	}
	if err := s.db.R.QueryRow("SELECT COUNT(*) FROM ledger WHERE reason = 'promo'").Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if uses != 3 || rows != 3 {
		t.Fatalf("ждали 3 активации и 3 начисления, получили %d и %d", uses, rows)
	}
}
