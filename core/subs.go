package core

import (
	"context"
	"time"
)

const day = 24 * time.Hour

// Sub отдаёт подписку абонента. Нет подписки: ErrNotFound.
func (c *Core) Sub(ctx context.Context, userID int64) (Subscription, error) {
	return c.store.SubByUser(ctx, userID)
}

// activeUser берёт абонента и отказывает, если он забанен.
func activeUser(tx Tx, userID int64) (User, error) {
	u, err := tx.User(userID)
	if err != nil {
		return u, err
	}
	if !u.BannedAt.IsZero() {
		return u, ErrBanned
	}
	return u, nil
}
