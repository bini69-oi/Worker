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
	var out LedgerEntry
	err := c.tx(ctx, func(tx Tx) error {
		if _, err := c.can(ctx, tx, adminID, PermGrant); err != nil {
			return err
		}
		now := c.clock.Now()
		var err error
		out, err = move(tx, now, LedgerEntry{UserID: userID, Amount: amount, Reason: ReasonAdmin, AdminID: adminID, Comment: comment})
		if err != nil {
			return err
		}
		return c.audit(ctx, tx, adminID, "balance.adjust", itoa(userID),
			map[string]string{"amount": itoa(amount), "comment": comment}, now)
	})
	return out, err
}
