package core

import "context"

// Payments отдаёт платежи всех касс страницами по PageSize с фильтром по статусу, кассе и датам.
// Нужно право stats. Новые платежи сверху.
func (c *Core) Payments(ctx context.Context, actorTg int64, f PaymentFilter, cursor int64) (Page[PaymentRow], error) {
	var page Page[PaymentRow]
	if _, err := c.Require(ctx, actorTg, PermStats, "payment.list"); err != nil {
		return page, err
	}
	switch f.Status {
	case "", PayPending, PayPaid, PayExpired, PayRefunded, PayFailed:
	default:
		return page, ErrInvalid
	}
	f.After = cursor
	rows, err := c.store.Payments(ctx, f, PageSize+1)
	if err != nil {
		return page, err
	}
	page.Items = rows
	if len(rows) > PageSize {
		page.Items = rows[:PageSize]
		page.Next = page.Items[PageSize-1].Payment.ID
	}
	return page, nil
}
