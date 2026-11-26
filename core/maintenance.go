package core

import "context"

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
	staff := false
	_ = c.store.Tx(ctx, func(tx Tx) error {
		_, err := c.admin(tx, u.TgID)
		staff = err == nil
		return nil
	})
	if staff {
		return nil
	}
	return ErrMaintenance
}

// Maintenance говорит, идут ли техработы. Бот и кабинет по нему показывают текст о работах.
func (c *Core) Maintenance() bool { return c.cfg.Bool("maintenance.on") }
