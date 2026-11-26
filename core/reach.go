package core

import (
	"context"
	"time"
)

// MarkUnreachable отмечает, что абонент заблокировал бота: новые уведомления ему не ставятся.
func (c *Core) MarkUnreachable(ctx context.Context, userID int64) error {
	return c.tx(ctx, func(tx Tx) error {
		u, err := tx.User(userID)
		if err != nil || !u.UnreachableAt.IsZero() {
			return err
		}
		return tx.SetUnreachable(userID, c.clock.Now())
	})
}

// MarkReachable снимает отметку, когда абонент снова написал боту.
func (c *Core) MarkReachable(ctx context.Context, userID int64) error {
	return c.tx(ctx, func(tx Tx) error {
		u, err := tx.User(userID)
		if err != nil || u.UnreachableAt.IsZero() {
			return err
		}
		return tx.SetUnreachable(userID, time.Time{})
	})
}

// SetScreen запоминает сообщение-экран абонента, которое бот правит при переходах.
func (c *Core) SetScreen(ctx context.Context, userID, msgID int64) error {
	return c.tx(ctx, func(tx Tx) error { return tx.SetScreenMsg(userID, msgID) })
}

// UserByTg находит абонента по Telegram ID. Только чтение: бот зовёт его на каждое обновление.
func (c *Core) UserByTg(ctx context.Context, tgID int64) (User, error) {
	return c.store.UserByTg(ctx, tgID)
}
