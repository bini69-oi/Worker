package core

import (
	"context"
	"errors"
	"time"
)

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

// SetPromoActive включает или выключает промокод. Нужно право promo (для денежного ещё grant). Чтение и запись
// идут одной транзакцией. Выключенный код не работает, число активаций остаётся.
func (c *Core) SetPromoActive(ctx context.Context, actorTg, id int64, active bool) (Promo, error) {
	var out Promo
	err := c.asAdmin(ctx, actorTg, PermPromo, "promo.save", func(tx Tx, a Admin, now time.Time) (string, map[string]string, error) {
		p, err := tx.PromoByID(id)
		if err != nil {
			return "", nil, err
		}
		p.Active = active
		out = p
		return c.savePromoTx(tx, a, &out, now)
	})
	if errors.Is(err, errNeedGrant) {
		c.deny(ctx, actorTg, "promo.save.money")
		return out, ErrForbidden
	}
	return out, err
}

// PanelSquad это сквад панели: группа серверов, которую выдаёт тариф.
type PanelSquad struct {
	UUID    string
	Name    string
	Members int
}

// SquadLister это панель, которая умеет отдать список сквадов. Это отдельный интерфейс, а не часть Panel:
// остальные сценарии ядра сквады не читают.
type SquadLister interface {
	Squads(ctx context.Context) ([]PanelSquad, error)
}

// PanelSquads отдаёт сквады панели для выбора в тарифе. Нужно право tariffs. Панель не ответила: ErrPanelDown,
// страница тарифов при этом работает и без списка. Панель без этой возможности отдаёт пустой список.
func (c *Core) PanelSquads(ctx context.Context, actorTg int64) ([]PanelSquad, error) {
	if _, err := c.Require(ctx, actorTg, PermTariffs, "tariff.squads"); err != nil {
		return nil, err
	}
	l, ok := c.panel.(SquadLister)
	if !ok {
		return nil, nil
	}
	list, err := l.Squads(ctx)
	return list, c.panelErr("squads", err)
}
