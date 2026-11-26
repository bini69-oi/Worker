package core

import "context"

// Serving это то же правило для бота и кабинета: они зовут его на входе и показывают текст о работах.
func (c *Core) Serving(ctx context.Context, userID int64) error { return c.serving(ctx, userID) }

// serving пускает абонента к сценарию. Во время техработ (maintenance.on) отказывает всем, кроме админов:
// они проверяют бота, пока абоненты ждут. Проверка стоит только на входах абонента, поэтому зачисление оплат,
// списание Stars-подписки, автопродление и синхронизация с панелью работают и во время техработ.
func (c *Core) serving(ctx context.Context, userID int64) error {
	if !c.cfg.Bool("maintenance.on") {
		return nil
	}
	u, err := c.store.User(ctx, userID)
	if err != nil {
		return err
	}
	if c.IsStaff(ctx, u.TgID) {
		return nil
	}
	return ErrMaintenance
}

// IsStaff говорит, админ ли это: владелец или помощник хоть с одним правом.
func (c *Core) IsStaff(ctx context.Context, tgID int64) bool {
	staff := false
	_ = c.store.Tx(ctx, func(tx Tx) error {
		_, err := c.admin(tx, tgID)
		staff = err == nil
		return nil
	})
	return staff
}

// Maintenance говорит, идут ли техработы. Бот и кабинет по нему показывают текст о работах.
func (c *Core) Maintenance() bool { return c.cfg.Bool("maintenance.on") }
