package core

import (
	"context"
	"errors"
	"time"
)

// maxGrantDays это предел дней за одну выдачу: опечатка «3000» не подарит восемь лет.
const maxGrantDays = 365

// GrantDays продлевает текущую подписку абонента на days дней от имени админа. Нужно право grant.
// Живая подписка продлевается от даты окончания, истёкшая от сегодня. Без подписки дни не выдаются:
// тариф выбирает админ в админке. Когда дни выдаёт помощник, владелец получает тревогу.
func (c *Core) GrantDays(ctx context.Context, actorTg, userID int64, days int) (Subscription, error) {
	if days < 1 || days > maxGrantDays {
		return Subscription{}, ErrInvalid
	}
	var out Subscription
	err := c.asAdmin(ctx, actorTg, PermGrant, "sub.grant_days", func(tx Tx, a Admin, now time.Time) (string, map[string]string, error) {
		sub, err := tx.SubByUser(userID)
		if errors.Is(err, ErrNotFound) {
			return "", nil, ErrNoSub
		}
		if err != nil {
			return "", nil, err
		}
		base := now
		if sub.Status == SubActive && sub.ExpiresAt.After(now) {
			base = sub.ExpiresAt
		}
		sub.ExpiresAt = base.Add(time.Duration(days) * day)
		sub.Status, sub.UpdatedAt = SubActive, now
		if _, err := tx.SaveSub(sub); err != nil {
			return "", nil, err
		}
		if err := enqueueSync(tx, sub.ID, now); err != nil {
			return "", nil, err
		}
		if !a.Owner {
			vars := map[string]string{"admin": itoa(a.TgID), "user_id": itoa(userID), "days": itoa(int64(days))}
			if err := c.alert(tx, "admin.helper_days", vars, now); err != nil {
				return "", nil, err
			}
		}
		out = sub
		return itoa(userID), map[string]string{"days": itoa(int64(days))}, nil
	})
	return out, err
}
