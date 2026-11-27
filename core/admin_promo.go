package core

import "context"

// Promos отдаёт промокоды с числом активаций страницами по PageSize, новые первыми. Нужно право promo.
func (c *Core) Promos(ctx context.Context, actorTg, cursor int64) (Page[Promo], error) {
	var page Page[Promo]
	if _, err := c.Require(ctx, actorTg, PermPromo, "promo.list"); err != nil {
		return page, err
	}
	list, err := c.store.PromoList(ctx, cursor, PageSize+1)
	if err != nil {
		return page, err
	}
	page.Items = list
	if len(list) > PageSize {
		page.Items = list[:PageSize]
		page.Next = page.Items[PageSize-1].ID
	}
	return page, nil
}

// SetPromoActive включает или выключает промокод. Нужно право promo. Выключенный код не работает,
// число активаций остаётся.
func (c *Core) SetPromoActive(ctx context.Context, actorTg, id int64, active bool) (Promo, error) {
	if _, err := c.Require(ctx, actorTg, PermPromo, "promo.save"); err != nil {
		return Promo{}, err
	}
	p, err := c.store.PromoByID(ctx, id)
	if err != nil {
		return p, err
	}
	p.Active = active
	return c.SavePromo(ctx, actorTg, p)
}
