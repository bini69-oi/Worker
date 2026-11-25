package core

import (
	"context"
	"errors"
	"time"
)

// Trial выдаёт пробный период. Один раз за всё время и только тому, у кого ещё не было подписки.
// Всем вместе не больше trial.max_per_hour за час: пачка новых аккаунтов Telegram не разберёт серверы,
// а владелец узнает о потоке тревогой.
func (c *Core) Trial(ctx context.Context, userID int64) (Subscription, error) {
	if !c.cfg.Bool("trial.enabled") {
		return Subscription{}, ErrTrialOff
	}
	days := int(c.cfg.Int("trial.days"))
	if days <= 0 {
		return Subscription{}, ErrTrialOff
	}
	var out Subscription
	full := false
	err := c.tx(ctx, func(tx Tx) error {
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
		if max := int(c.cfg.Int("trial.max_per_hour")); max > 0 {
			if err := countTry(tx, "trial:all", max, time.Hour, now); err != nil {
				full = errors.Is(err, ErrTooManyTries)
				return err
			}
		}
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
	if full {
		now := c.clock.Now()
		_ = c.tx(ctx, func(tx Tx) error {
			first, err := tx.AddNotice(0, "trial_limit", now.Truncate(time.Hour).Format(time.RFC3339), now)
			if err != nil || !first {
				return err
			}
			return c.alert(tx, "admin.trial_limit", map[string]string{"max": itoa(c.cfg.Int("trial.max_per_hour"))}, now)
		})
		return Subscription{}, ErrTrialBusy
	}
	return out, err
}
