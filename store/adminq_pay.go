package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/bini69-oi/worker/core"
)

func (s *CoreStore) UserPayments(ctx context.Context, userID int64, limit int) ([]core.Payment, error) {
	rows, err := s.db.R.QueryContext(ctx,
		"SELECT "+payCols+" FROM payments WHERE user_id = ? ORDER BY id DESC LIMIT ?", userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []core.Payment
	for rows.Next() {
		p, err := scanPayment(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *CoreStore) Payments(ctx context.Context, f core.PaymentFilter, limit int) ([]core.PaymentRow, error) {
	where, args := []string{"1 = 1"}, []any{}
	add := func(cond string, v any) { where, args = append(where, cond), append(args, v) }
	if f.After > 0 {
		add("p.id < ?", f.After)
	}
	if f.Status != "" {
		add("p.status = ?", string(f.Status))
	}
	if f.Gateway != "" {
		add("p.gateway = ?", f.Gateway)
	}
	if !f.From.IsZero() {
		add("p.created_at >= ?", f.From.Unix())
	}
	if !f.To.IsZero() {
		add("p.created_at < ?", f.To.Unix())
	}
	rows, err := s.db.R.QueryContext(ctx,
		"SELECT p.id, p.user_id, p.gateway, p.amount, p.ext_amount, p.ext_currency, p.recurring, p.status, p.external_id, p.created_at, p.paid_at, p.refunded_at, u.username, u.tg_id "+
			"FROM payments p JOIN users u ON u.id = p.user_id WHERE "+strings.Join(where, " AND ")+" ORDER BY p.id DESC LIMIT ?",
		append(args, limit)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []core.PaymentRow
	for rows.Next() {
		var r core.PaymentRow
		var status string
		var ext sql.NullString
		var created int64
		var paid, refunded sql.NullInt64
		p := &r.Payment
		if err := rows.Scan(&p.ID, &p.UserID, &p.Gateway, &p.Amount, &p.ExtAmount, &p.ExtCurrency, &p.Recurring, &status, &ext,
			&created, &paid, &refunded, &r.Username, &r.TgID); err != nil {
			return nil, err
		}
		p.Status, p.ExternalID, p.CreatedAt, p.PaidAt, p.RefundedAt = core.PayStatus(status), ext.String, time.Unix(created, 0).UTC(), unix(paid), unix(refunded)
		out = append(out, r)
	}
	return out, rows.Err()
}

func scanPromo(r scanner) (core.Promo, error) {
	var p core.Promo
	var kind string
	var tariff, expires sql.NullInt64
	var created int64
	if err := r.Scan(&p.ID, &p.Code, &kind, &p.Value, &tariff, &p.MaxUses, &p.Uses, &p.NewOnly, &expires, &p.Active, &created); err != nil {
		return p, err
	}
	p.Kind, p.TariffID, p.ExpiresAt, p.CreatedAt = core.PromoKind(kind), tariff.Int64, unix(expires), time.Unix(created, 0).UTC()
	return p, nil
}

func (s *CoreStore) PromoList(ctx context.Context, after int64, limit int) ([]core.Promo, error) {
	q, args := "SELECT "+promoCols+" FROM promo_codes", []any{}
	if after > 0 {
		q, args = q+" WHERE id < ?", append(args, after)
	}
	rows, err := s.db.R.QueryContext(ctx, q+" ORDER BY id DESC LIMIT ?", append(args, limit)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []core.Promo
	for rows.Next() {
		p, err := scanPromo(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *CoreStore) PromoByID(ctx context.Context, id int64) (core.Promo, error) {
	p, err := scanPromo(s.db.R.QueryRowContext(ctx, "SELECT "+promoCols+" FROM promo_codes WHERE id = ?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return p, core.ErrNotFound
	}
	return p, err
}

func (s *CoreStore) GatewayStats(ctx context.Context) (map[string]core.GatewayStat, error) {
	rows, err := s.db.R.QueryContext(ctx,
		`SELECT gateway, MAX(CASE WHEN status = 'paid' THEN paid_at END), SUM(status = 'pending') FROM payments GROUP BY gateway`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]core.GatewayStat{}
	for rows.Next() {
		var name string
		var paid sql.NullInt64
		var pending sql.NullInt64
		if err := rows.Scan(&name, &paid, &pending); err != nil {
			return nil, err
		}
		out[name] = core.GatewayStat{LastPaidAt: unix(paid), Pending: int(pending.Int64)}
	}
	return out, rows.Err()
}

func (t *coreTx) PromoByID(id int64) (core.Promo, error) {
	p, err := scanPromo(t.q.QueryRowContext(t.ctx, "SELECT "+promoCols+" FROM promo_codes WHERE id = ?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return p, core.ErrNotFound
	}
	return p, err
}
