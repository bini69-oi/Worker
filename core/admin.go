package core

import (
	"context"
	"strings"
)

// AdminAdjust начисляет (плюс) или списывает (минус) деньги абоненту от имени админа.
// Комментарий обязателен: в истории видно, кто и почему.
func (c *Core) AdminAdjust(ctx context.Context, adminID, userID, amount int64, comment string) (LedgerEntry, error) {
	comment = strings.TrimSpace(comment)
	if comment == "" {
		return LedgerEntry{}, ErrNeedComment
	}
	if adminID <= 0 {
		return LedgerEntry{}, ErrForbidden
	}
	var out LedgerEntry
	err := c.store.Tx(ctx, func(tx Tx) error {
		var err error
		out, err = move(tx, c.clock.Now(), LedgerEntry{
			UserID:  userID,
			Amount:  amount,
			Reason:  ReasonAdmin,
			AdminID: adminID,
			Comment: comment,
		})
		// Запись в журнал действий админов встанет сюда же, в эту транзакцию (группа 11).
		return err
	})
	return out, err
}
