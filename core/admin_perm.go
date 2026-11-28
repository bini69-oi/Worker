package core

import (
	"context"
	"errors"
	"slices"
)

// PermOwner это «только владелец» в таблице прав адресов API. Помощнику его выдать нельзя.
const PermOwner Perm = "owner"

// Require проверяет право админа на адрес API: пустое право значит любого админа, PermOwner только владельца.
// Отказ пишется в журнал и уходит владельцу тревогой. Сами функции ядра проверяют право ещё раз:
// эта проверка нужна, чтобы чужой запрос не дошёл даже до разбора тела.
func (c *Core) Require(ctx context.Context, tgID int64, p Perm, action string) (Admin, error) {
	// Проверка права это чтение: транзакция записи на каждый запрос не нужна.
	row, err := c.store.AdminByTg(ctx, tgID)
	a, err := c.resolveAdmin(row, err, tgID)
	if err == nil {
		switch {
		case p == "":
		case p == PermOwner && !a.Owner:
			err = ErrOwnerOnly
		case p != PermOwner && !a.Owner && !slices.Contains(a.Perms, p):
			err = ErrForbidden
		}
	}
	if errors.Is(err, ErrForbidden) || errors.Is(err, ErrOwnerOnly) {
		c.deny(ctx, tgID, action)
	}
	return a, err
}
