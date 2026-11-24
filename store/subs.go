package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/bini69-oi/worker/core"
)

const subCols = `id, user_id, tariff_id, period_days, price, trial, panel_uuid, sub_url,
	expires_at, auto_renew, status, created_at, updated_at`

func (s *CoreStore) SubByUser(ctx context.Context, userID int64) (core.Subscription, error) {
	return (&coreTx{ctx: ctx, q: s.db.R}).SubByUser(userID)
}

func (t *coreTx) SubByUser(userID int64) (core.Subscription, error) {
	sub, err := scanSub(t.q.QueryRowContext(t.ctx, "SELECT "+subCols+" FROM subscriptions WHERE user_id = ?", userID))
	if errors.Is(err, sql.ErrNoRows) {
		return sub, core.ErrNotFound
	}
	return sub, err
}

// SaveSub создаёт подписку (ID == 0) или обновляет её. Вторая подписка того же абонента даёт ErrDuplicate.
func (t *coreTx) SaveSub(s core.Subscription) (int64, error) {
	if s.ID == 0 {
		res, err := t.q.ExecContext(t.ctx,
			`INSERT INTO subscriptions (user_id, tariff_id, period_days, price, trial, panel_uuid, sub_url,
				expires_at, auto_renew, status, created_at, updated_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			s.UserID, nullID(s.TariffID), s.PeriodDays, s.Price, s.Trial, s.PanelUUID, s.SubURL,
			s.ExpiresAt.Unix(), s.AutoRenew, string(s.Status), s.CreatedAt.Unix(), s.UpdatedAt.Unix())
		if err != nil {
			return 0, mapErr(err)
		}
		return res.LastInsertId()
	}
	res, err := t.q.ExecContext(t.ctx,
		`UPDATE subscriptions SET tariff_id = ?, period_days = ?, price = ?, trial = ?, panel_uuid = ?, sub_url = ?,
			expires_at = ?, auto_renew = ?, status = ?, updated_at = ?
		 WHERE id = ?`,
		nullID(s.TariffID), s.PeriodDays, s.Price, s.Trial, s.PanelUUID, s.SubURL,
		s.ExpiresAt.Unix(), s.AutoRenew, string(s.Status), s.UpdatedAt.Unix(), s.ID)
	if err != nil {
		return 0, mapErr(err)
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return 0, core.ErrNotFound
	}
	return s.ID, nil
}

func (t *coreTx) ActiveSubsUntil(until time.Time) ([]core.Subscription, error) {
	rows, err := t.q.QueryContext(t.ctx,
		"SELECT "+subCols+" FROM subscriptions WHERE status = 'active' AND expires_at <= ? ORDER BY id", until.Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []core.Subscription
	for rows.Next() {
		s, err := scanSub(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (t *coreTx) SetTrialUsed(userID int64) error {
	res, err := t.q.ExecContext(t.ctx, "UPDATE users SET trial_used = 1 WHERE id = ?", userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return core.ErrNotFound
	}
	return nil
}

func scanSub(r scanner) (core.Subscription, error) {
	var s core.Subscription
	var tariff sql.NullInt64
	var status string
	var expires, created, updated int64
	err := r.Scan(&s.ID, &s.UserID, &tariff, &s.PeriodDays, &s.Price, &s.Trial, &s.PanelUUID, &s.SubURL,
		&expires, &s.AutoRenew, &status, &created, &updated)
	if err != nil {
		return s, err
	}
	s.TariffID = tariff.Int64
	s.Status = core.SubStatus(status)
	s.ExpiresAt = time.Unix(expires, 0).UTC()
	s.CreatedAt = time.Unix(created, 0).UTC()
	s.UpdatedAt = time.Unix(updated, 0).UTC()
	return s, nil
}
