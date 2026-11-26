package core

import "context"

// Quote это цена покупки для экрана подтверждения: сколько стоит, сколько есть и сколько не хватает.
type Quote struct {
	Price   int64 // копейки
	Balance int64 // копейки
	Missing int64 // копейки, 0 если денег хватает
}

// Quote считает цену покупки по тем же правилам, что Buy, но ничего не списывает.
func (c *Core) Quote(ctx context.Context, userID, tariffID int64, days int) (Quote, error) {
	var q Quote
	err := c.store.Tx(ctx, func(tx Tx) error {
		b, err := planBuy(tx, userID, tariffID, days, c.clock.Now())
		if err != nil {
			return err
		}
		q.Price, q.Balance = b.period.Price, b.buyer.Balance
		q.Missing = max(q.Price-q.Balance, 0)
		return nil
	})
	return q, err
}
