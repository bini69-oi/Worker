package core

import (
	"context"
	"errors"
	"time"
)

// Trial выдаёт пробный период. Один раз за всё время и только тому, у кого ещё не было подписки.
func (c *Core) Trial(ctx context.Context, userID int64) (Subscription, error) {
	if !c.cfg.Bool("trial.enabled") {
		return Subscription{}, ErrTrialOff
	}
	days := int(c.cfg.Int("trial.days"))
	if days <= 0 {
		return Subscription{}, ErrTrialOff
	}
	var out Subscription
	err := c.store.Tx(ctx, func(tx Tx) error {
		u, err := activeUser(tx, userID)
		if err != nil {
			return err
		}
		if u.TrialUsed {
			return ErrTrialUsed
		}
		if _, err := tx.SubByUser(userID); !errors.Is(err, ErrNotFound) {
			if err != nil {
				return err
			}
			return ErrTrialUsed
		}
		now := c.clock.Now()
		out = Subscription{
			UserID: userID, PeriodDays: days, Trial: true, Status: SubActive,
			ExpiresAt: now.Add(time.Duration(days) * day), CreatedAt: now, UpdatedAt: now,
		}
		if out.ID, err = tx.SaveSub(out); err != nil {
			return err
		}
		if err := tx.SetTrialUsed(userID); err != nil {
			return err
		}
		return enqueueSync(tx, out.ID, now)
	})
	return out, err
}
