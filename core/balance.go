package core

import (
	"context"
	"errors"
	"time"
)

// move двигает деньги внутри транзакции: проверяет, что баланс не уйдёт в минус,
// и пишет строку истории вместе с кэшем баланса. Все движения денег идут только через move.
func move(tx Tx, now time.Time, e LedgerEntry) (LedgerEntry, error) {
	if e.Amount == 0 {
		return e, ErrZeroAmount
	}
	u, err := tx.User(e.UserID)
	if err != nil {
		return e, err
	}
	if u.Balance+e.Amount < 0 {
		return e, ErrNotEnoughMoney
	}
	e.CreatedAt = now
	id, err := tx.AddLedger(e)
	if err != nil {
		return e, err
	}
	e.ID = id
	return e, nil
}

// Register находит абонента по Telegram ID или создаёт нового. Повторный вызов отдаёт того же абонента.
func (c *Core) Register(ctx context.Context, tgID int64, username, lang string) (User, error) {
	return c.RegisterRef(ctx, tgID, username, lang, "")
}

// RegisterRef как Register, но новый абонент запоминает пригласившего по коду из реферальной ссылки.
// Существующий абонент пригласившего не меняет.
func (c *Core) RegisterRef(ctx context.Context, tgID int64, username, lang, refCode string) (User, error) {
	if tgID <= 0 {
		return User{}, ErrInvalid
	}
	if lang == "" {
		lang = "ru"
	}
	var out User
	err := c.tx(ctx, func(tx Tx) error {
		u, err := tx.UserByTg(tgID)
		if err == nil {
			out = u
			return nil
		}
		if !errors.Is(err, ErrNotFound) {
			return err
		}
		out, err = c.registerTx(tx, tgID, username, lang, refCode, c.clock.Now())
		return err
	})
	return out, err
}

// History отдаёт последние движения денег абонента.
func (c *Core) History(ctx context.Context, userID int64, limit int) ([]LedgerEntry, error) {
	if limit <= 0 || limit > 100 {
		limit = 100
	}
	return c.store.Ledger(ctx, userID, limit)
}
