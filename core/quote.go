package core

import (
	"context"
	"time"
)

// Quote это цена покупки для экрана подтверждения: сколько стоит, сколько есть и сколько не хватает.
type Quote struct {
	Price   int64 // копейки
	Balance int64 // копейки
	Missing int64 // копейки, 0 если денег хватает
	// Queued значит, что это другой тариф при живой подписке: он начнётся StartsAt, когда закончится текущий.
	Queued   bool
	StartsAt time.Time
}

// Quote считает цену покупки по тем же правилам, что Buy, но ничего не списывает.
func (c *Core) Quote(ctx context.Context, userID, tariffID int64, days int) (Quote, error) {
	var q Quote
	err := c.store.Tx(ctx, func(tx Tx) error {
		b, err := planBuy(c, tx, userID, tariffID, days, c.clock.Now())
		if err != nil {
			return err
		}
		q.Price, q.Balance = b.cost, b.buyer.Balance
		q.Missing = max(q.Price-q.Balance, 0)
		if b.queue {
			q.Queued, q.StartsAt = true, b.sub.ExpiresAt
		}
		return nil
	})
	return q, err
}
