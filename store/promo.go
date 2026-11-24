package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/bini69-oi/worker/core"
)

const promoCols = `id, code, kind, value, tariff_id, max_uses, uses, new_only, expires_at, active, created_at`

// Promo ищет код без учёта регистра: у колонки code правило сравнения NOCASE.
func (t *coreTx) Promo(code string) (core.Promo, error) {
	var p core.Promo
	var kind string
	var tariff, expires sql.NullInt64
	var created int64
	err := t.q.QueryRowContext(t.ctx, "SELECT "+promoCols+" FROM promo_codes WHERE code = ?", code).
		Scan(&p.ID, &p.Code, &kind, &p.Value, &tariff, &p.MaxUses, &p.Uses, &p.NewOnly, &expires, &p.Active, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return p, core.ErrNotFound
	}
	if err != nil {
		return p, err
	}
	p.Kind, p.TariffID = core.PromoKind(kind), tariff.Int64
	if expires.Valid {
		p.ExpiresAt = time.Unix(expires.Int64, 0).UTC()
	}
	p.CreatedAt = time.Unix(created, 0).UTC()
	return p, nil
}

func nullTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t.Unix()
}

// SavePromo создаёт или меняет код. Счётчик активаций при правке не трогается.
func (t *coreTx) SavePromo(p core.Promo) (int64, error) {
	if p.ID == 0 {
		res, err := t.q.ExecContext(t.ctx,
			`INSERT INTO promo_codes (code, kind, value, tariff_id, max_uses, new_only, expires_at, active, created_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			p.Code, string(p.Kind), p.Value, nullID(p.TariffID), p.MaxUses, p.NewOnly, nullTime(p.ExpiresAt), p.Active, p.CreatedAt.Unix())
		if err != nil {
			return 0, mapErr(err)
		}
		return res.LastInsertId()
	}
	res, err := t.q.ExecContext(t.ctx,
		`UPDATE promo_codes SET code = ?, kind = ?, value = ?, tariff_id = ?, max_uses = ?, new_only = ?, expires_at = ?, active = ?
		 WHERE id = ?`,
		p.Code, string(p.Kind), p.Value, nullID(p.TariffID), p.MaxUses, p.NewOnly, nullTime(p.ExpiresAt), p.Active, p.ID)
	if err != nil {
		return 0, mapErr(err)
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return 0, core.ErrNotFound
	}
	return p.ID, nil
}

// UsePromo пишет активацию и увеличивает счётчик условным обновлением: лимит не превысить даже одновременно.
func (t *coreTx) UsePromo(promoID, userID int64, at time.Time) error {
	if _, err := t.q.ExecContext(t.ctx,
		"INSERT INTO promo_uses (promo_id, user_id, used_at) VALUES (?, ?, ?)", promoID, userID, at.Unix()); err != nil {
		return mapErr(err)
	}
	res, err := t.q.ExecContext(t.ctx,
		"UPDATE promo_codes SET uses = uses + 1 WHERE id = ? AND (max_uses = 0 OR uses < max_uses)", promoID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return core.ErrPromoExhausted
	}
	return nil
}

func (t *coreTx) Limit(key string) (int, time.Time, error) {
	var count int
	var until int64
	err := t.q.QueryRowContext(t.ctx, "SELECT count, until FROM login_fails WHERE key = ?", key).Scan(&count, &until)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, time.Time{}, nil
	}
	if err != nil {
		return 0, time.Time{}, err
	}
	return count, time.Unix(until, 0).UTC(), nil
}

func (t *coreTx) SetLimit(key string, count int, until time.Time) error {
	_, err := t.q.ExecContext(t.ctx,
		`INSERT INTO login_fails (key, count, until) VALUES (?, ?, ?)
		 ON CONFLICT (key) DO UPDATE SET count = excluded.count, until = excluded.until`, key, count, until.Unix())
	return err
}

// RefStats считает приглашённых, купивших из них и заработанные бонусы.
func (s *CoreStore) RefStats(ctx context.Context, userID int64) (core.RefStats, error) {
	var st core.RefStats
	err := s.db.R.QueryRowContext(ctx, `
		SELECT
			(SELECT COUNT(*) FROM users WHERE referrer_id = ?1),
			(SELECT COUNT(DISTINCT l.user_id) FROM ledger l JOIN users u ON u.id = l.user_id
			 WHERE u.referrer_id = ?1 AND l.reason = 'buy'),
			(SELECT COALESCE(SUM(amount), 0) FROM ledger WHERE user_id = ?1 AND reason = 'referral' AND ref_type = 'ref_buy')`,
		userID).Scan(&st.Invited, &st.Bought, &st.Earned)
	return st, err
}
