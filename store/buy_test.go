package store

import (
	"context"
	"errors"
	"testing"

	"github.com/bini69-oi/worker/core"
	"github.com/bini69-oi/worker/core/coretest"
)

// TestBuyOnSQLite прогоняет покупку ядра на настоящей базе: деньги, подписка и задача в очереди
// пишутся одной транзакцией, а отказ ничего не оставляет.
func TestBuyOnSQLite(t *testing.T) {
	s, uid := newCoreStore(t)
	ctx := context.Background()
	c := core.New(core.Deps{Store: s, Clock: coretest.NewClock(tNow), Config: coretest.NewConfig(t), OwnerID: 1})
	tr, err := c.SaveTariff(ctx, core.Tariff{Name: "Месяц", Active: true, Periods: []core.Period{{Days: 30, Price: 15000}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Buy(ctx, uid, tr.ID, 30); !errors.Is(err, core.ErrNotEnoughMoney) {
		t.Fatalf("без денег: ждали ErrNotEnoughMoney, получили %v", err)
	}
	if _, err := c.AdminAdjust(ctx, 1, uid, 20000, "тест"); err != nil {
		t.Fatal(err)
	}
	sub, err := c.Buy(ctx, uid, tr.ID, 30)
	if err != nil {
		t.Fatal(err)
	}
	var tasks int
	if err := s.db.R.QueryRow("SELECT COUNT(*) FROM outbox WHERE kind = ?", core.KindPanelSync).Scan(&tasks); err != nil {
		t.Fatal(err)
	}
	u, _ := s.User(ctx, uid)
	if u.Balance != 5000 || sub.Status != core.SubActive || tasks != 1 {
		t.Fatalf("баланс %d, подписка %s, задач %d", u.Balance, sub.Status, tasks)
	}
}
