package core

import (
	"context"
	"time"
)

// MarkUnreachable отмечает, что абонент заблокировал бота: новые уведомления ему не ставятся.
func (c *Core) MarkUnreachable(ctx context.Context, userID int64) error {
	return c.setReach(ctx, userID, false)
}

// MarkReachable снимает отметку, когда абонент снова написал боту.
func (c *Core) MarkReachable(ctx context.Context, userID int64) error {
	return c.setReach(ctx, userID, true)
}

// setReach меняет отметку, только если она другая: первое время блокировки не сдвигается.
func (c *Core) setReach(ctx context.Context, userID int64, reachable bool) error {
	return c.tx(ctx, func(tx Tx) error {
		u, err := tx.User(userID)
		if err != nil || u.UnreachableAt.IsZero() == reachable {
			return err
		}
		at := time.Time{}
		if !reachable {
			at = c.clock.Now()
		}
		return tx.SetUnreachable(userID, at)
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

// SetLang меняет язык абонента. Код языка вида ru, en или pt-br.
func (c *Core) SetLang(ctx context.Context, userID int64, lang string) error {
	if !validLang(lang) {
		return ErrInvalid
	}
	return c.tx(ctx, func(tx Tx) error { return tx.SetLang(userID, lang) })
}

func validLang(s string) bool {
	if len(s) < 2 || len(s) > 8 {
		return false
	}
	for i, r := range s {
		if !(r >= 'a' && r <= 'z' || i > 1 && (r == '-' || r >= '0' && r <= '9')) {
			return false
		}
	}
	return true
}

// User отдаёт абонента по id. Только чтение.
func (c *Core) User(ctx context.Context, id int64) (User, error) { return c.store.User(ctx, id) }
