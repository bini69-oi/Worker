package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/bini69-oi/worker/core"
)

const subCols = `id, user_id, tariff_id, period_days, price, trial, panel_uuid, sub_url,
	expires_at, auto_renew, status, created_at, updated_at,
	next_tariff_id, next_days, next_price, extra_traffic_bytes, extra_devices,
	imported, own_username, own_traffic_bytes, own_devices, own_reset, own_squads`

func (s *CoreStore) SubByUser(ctx context.Context, userID int64) (core.Subscription, error) {
	return (&coreTx{ctx: ctx, q: s.db.R}).SubByUser(userID)
}

func (t *coreTx) Sub(id int64) (core.Subscription, error) {
	sub, err := scanSub(t.q.QueryRowContext(t.ctx, "SELECT "+subCols+" FROM subscriptions WHERE id = ?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return sub, core.ErrNotFound
	}
	return sub, err
}

func (t *coreTx) SubByUser(userID int64) (core.Subscription, error) {
	sub, err := scanSub(t.q.QueryRowContext(t.ctx, "SELECT "+subCols+" FROM subscriptions WHERE user_id = ?", userID))
	if errors.Is(err, sql.ErrNoRows) {
		return sub, core.ErrNotFound
	}
	return sub, err
}

func (t *coreTx) SubByPanel(ref string) (core.Subscription, error) {
	if ref == "" {
		return core.Subscription{}, core.ErrNotFound
	}
	sub, err := scanSub(t.q.QueryRowContext(t.ctx, "SELECT "+subCols+" FROM subscriptions WHERE panel_uuid = ?", ref))
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
				expires_at, auto_renew, status, created_at, updated_at,
				next_tariff_id, next_days, next_price, extra_traffic_bytes, extra_devices,
				imported, own_username, own_traffic_bytes, own_devices, own_reset, own_squads)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			s.UserID, nullID(s.TariffID), s.PeriodDays, s.Price, s.Trial, s.PanelUUID, s.SubURL,
			s.ExpiresAt.Unix(), s.AutoRenew, string(s.Status), s.CreatedAt.Unix(), s.UpdatedAt.Unix(),
			s.NextTariffID, s.NextDays, s.NextPrice, s.ExtraTrafficBytes, s.ExtraDevices,
			s.Imported, s.Own.Username, s.Own.TrafficBytes, s.Own.Devices, s.Own.Reset, squadsJSON(s.Own.Squads))
		if err != nil {
			return 0, mapErr(err)
		}
		return res.LastInsertId()
	}
	res, err := t.q.ExecContext(t.ctx,
		`UPDATE subscriptions SET tariff_id = ?, period_days = ?, price = ?, trial = ?, panel_uuid = ?, sub_url = ?,
			expires_at = ?, auto_renew = ?, status = ?, updated_at = ?,
			next_tariff_id = ?, next_days = ?, next_price = ?, extra_traffic_bytes = ?, extra_devices = ?,
			imported = ?, own_username = ?, own_traffic_bytes = ?, own_devices = ?, own_reset = ?, own_squads = ?
		 WHERE id = ?`,
		nullID(s.TariffID), s.PeriodDays, s.Price, s.Trial, s.PanelUUID, s.SubURL,
		s.ExpiresAt.Unix(), s.AutoRenew, string(s.Status), s.UpdatedAt.Unix(),
		s.NextTariffID, s.NextDays, s.NextPrice, s.ExtraTrafficBytes, s.ExtraDevices,
		s.Imported, s.Own.Username, s.Own.TrafficBytes, s.Own.Devices, s.Own.Reset, squadsJSON(s.Own.Squads), s.ID)
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
	var ownSquads string
	err := r.Scan(&s.ID, &s.UserID, &tariff, &s.PeriodDays, &s.Price, &s.Trial, &s.PanelUUID, &s.SubURL,
		&expires, &s.AutoRenew, &status, &created, &updated,
		&s.NextTariffID, &s.NextDays, &s.NextPrice, &s.ExtraTrafficBytes, &s.ExtraDevices,
		&s.Imported, &s.Own.Username, &s.Own.TrafficBytes, &s.Own.Devices, &s.Own.Reset, &ownSquads)
	if err != nil {
		return s, err
	}
	s.TariffID = tariff.Int64
	s.Status = core.SubStatus(status)
	s.ExpiresAt = time.Unix(expires, 0).UTC()
	s.CreatedAt = time.Unix(created, 0).UTC()
	s.UpdatedAt = time.Unix(updated, 0).UTC()
	if err := json.Unmarshal([]byte(ownSquads), &s.Own.Squads); err != nil {
		return s, err
	}
	return s, nil
}

// squadsJSON пишет сквады строкой JSON. Ошибки быть не может: срез строк всегда кодируется.
func squadsJSON(s []string) string {
	b, _ := json.Marshal(nonNil(s))
	return string(b)
}
