package store

import (
	"database/sql"
	"errors"
	"time"

	"github.com/bini69-oi/worker/core"
)

// SetPendingBuy запоминает выбор абонента. Новый выбор заменяет старый.
func (t *coreTx) SetPendingBuy(pb core.PendingBuy) error {
	_, err := t.q.ExecContext(t.ctx,
		`INSERT INTO pending_buys (user_id, tariff_id, days, created_at) VALUES (?, ?, ?, ?)
		 ON CONFLICT (user_id) DO UPDATE SET tariff_id = excluded.tariff_id, days = excluded.days, created_at = excluded.created_at`,
		pb.UserID, pb.TariffID, pb.Days, pb.CreatedAt.Unix())
	return mapErr(err)
}

func (t *coreTx) PendingBuy(userID int64) (core.PendingBuy, error) {
	pb := core.PendingBuy{UserID: userID}
	var created int64
	err := t.q.QueryRowContext(t.ctx, "SELECT tariff_id, days, created_at FROM pending_buys WHERE user_id = ?", userID).
		Scan(&pb.TariffID, &pb.Days, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return pb, core.ErrNotFound
	}
	pb.CreatedAt = time.Unix(created, 0).UTC()
	return pb, err
}

func (t *coreTx) DeletePendingBuy(userID int64) error {
	_, err := t.q.ExecContext(t.ctx, "DELETE FROM pending_buys WHERE user_id = ?", userID)
	return err
}
