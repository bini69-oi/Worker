package core

import (
	"context"
	"strings"
	"time"
)

// AdminAdjust начисляет (плюс) или списывает (минус) деньги абоненту от имени админа.
// Комментарий обязателен: в истории видно, кто и почему. Когда деньги двигает помощник,
// владелец получает тревогу: угнанная сессия помощника не начислит себе тихо.
func (c *Core) AdminAdjust(ctx context.Context, adminID, userID, amount int64, comment string) (LedgerEntry, error) {
	comment = strings.TrimSpace(comment)
	if comment == "" {
		return LedgerEntry{}, ErrNeedComment
	}
	var out LedgerEntry
	err := c.asAdmin(ctx, adminID, PermGrant, "balance.adjust", func(tx Tx, a Admin, now time.Time) (string, map[string]string, error) {
		var err error
		out, err = move(tx, now, LedgerEntry{UserID: userID, Amount: amount, Reason: ReasonAdmin, AdminID: adminID, Comment: comment})
		if err != nil {
			return "", nil, err
		}
		details := map[string]string{"amount": itoa(amount), "comment": comment}
		return userTarget(userID), details, c.helperMoney(tx, a, "balance.adjust", userID, amount, now)
	})
	return out, err
}

// helperMoney шлёт владельцу тревогу, если деньги двинул помощник. Действия самого владельца не шумят.
func (c *Core) helperMoney(tx Tx, a Admin, action string, userID, amount int64, now time.Time) error {
	if a.Owner {
		return nil
	}
	return c.alert(tx, "admin.helper_money", map[string]string{
		"admin": itoa(a.TgID), "action": action, "user_id": itoa(userID), "amount": itoa(amount)}, now)
}
