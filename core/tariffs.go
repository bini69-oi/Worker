package core

import (
	"context"
	"strings"
)

// resetPeriods это способы сброса трафика, которые понимает Remnawave.
var resetPeriods = map[string]bool{"NO_RESET": true, "DAY": true, "WEEK": true, "MONTH": true}

// validTariff проверяет тариф перед сохранением.
func validTariff(t *Tariff) error {
	t.Name = strings.TrimSpace(t.Name)
	if t.Reset == "" {
		t.Reset = "NO_RESET"
	}
	if t.Name == "" || t.TrafficBytes < 0 || t.Devices < 0 || !resetPeriods[t.Reset] || len(t.Periods) == 0 {
		return ErrInvalid
	}
	seen := map[int]bool{}
	for _, p := range t.Periods {
		if p.Days <= 0 || p.Price < 0 || seen[p.Days] {
			return ErrInvalid
		}
		seen[p.Days] = true
	}
	return nil
}

// SaveTariff создаёт тариф (ID == 0) или меняет его. Уже проданные подписки не меняются: их цена заморожена.
func (c *Core) SaveTariff(ctx context.Context, t Tariff) (Tariff, error) {
	if err := validTariff(&t); err != nil {
		return t, err
	}
	if t.ID == 0 {
		t.CreatedAt = c.clock.Now()
	}
	err := c.tx(ctx, func(tx Tx) error {
		id, err := tx.SaveTariff(t)
		t.ID = id
		return err
	})
	return t, err
}

// Tariffs отдаёт тарифы для покупки: только активные, в порядке админа.
func (c *Core) Tariffs(ctx context.Context) ([]Tariff, error) {
	return c.store.Tariffs(ctx, true)
}

// AllTariffs отдаёт все тарифы, включая скрытые. Это для админки.
func (c *Core) AllTariffs(ctx context.Context) ([]Tariff, error) {
	return c.store.Tariffs(ctx, false)
}

// period ищет срок тарифа.
func (t Tariff) period(days int) (Period, bool) {
	for _, p := range t.Periods {
		if p.Days == days {
			return p, true
		}
	}
	return Period{}, false
}
