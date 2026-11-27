package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/bini69-oi/worker/core"
)

const payCols = `id, user_id, gateway, amount, ext_amount, ext_currency, recurring, status, external_id, created_at, paid_at, source`

func scanPayment(r scanner) (core.Payment, error) {
	var p core.Payment
	var status string
	var ext sql.NullString
	var created int64
	var paid sql.NullInt64
	err := r.Scan(&p.ID, &p.UserID, &p.Gateway, &p.Amount, &p.ExtAmount, &p.ExtCurrency, &p.Recurring, &status, &ext, &created, &paid, &p.Source)
	if err != nil {
		return p, err
	}
	p.Status = core.PayStatus(status)
	p.ExternalID = ext.String
	p.CreatedAt = time.Unix(created, 0).UTC()
	if paid.Valid {
		p.PaidAt = time.Unix(paid.Int64, 0).UTC()
	}
	return p, nil
}

// nullText превращает пустую строку в NULL: уникальный ключ (касса, номер) не мешает платежам без номера.
func nullText(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func (s *CoreStore) PendingPayments(ctx context.Context, before time.Time) ([]core.Payment, error) {
	rows, err := s.db.R.QueryContext(ctx,
		"SELECT "+payCols+" FROM payments WHERE status = 'pending' AND created_at < ? ORDER BY id", before.Unix())
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

func (t *coreTx) CreatePayment(p core.Payment) (int64, error) {
	res, err := t.q.ExecContext(t.ctx,
		`INSERT INTO payments (user_id, gateway, amount, ext_amount, ext_currency, recurring, status, external_id, created_at, source)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.UserID, p.Gateway, p.Amount, p.ExtAmount, p.ExtCurrency, p.Recurring, string(p.Status), nullText(p.ExternalID), p.CreatedAt.Unix(), p.Source)
	if err != nil {
		return 0, mapErr(err)
	}
	return res.LastInsertId()
}

func (t *coreTx) Paid(userID int64) (bool, error) {
	var ok bool
	err := t.q.QueryRowContext(t.ctx,
		"SELECT EXISTS (SELECT 1 FROM payments WHERE user_id = ? AND status = 'paid')", userID).Scan(&ok)
	return ok, err
}

func (t *coreTx) Payment(id int64) (core.Payment, error) {
	p, err := scanPayment(t.q.QueryRowContext(t.ctx, "SELECT "+payCols+" FROM payments WHERE id = ?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return p, core.ErrNotFound
	}
	return p, err
}

func (t *coreTx) PaymentByExt(gateway, externalID string) (core.Payment, error) {
	p, err := scanPayment(t.q.QueryRowContext(t.ctx,
		"SELECT "+payCols+" FROM payments WHERE gateway = ? AND external_id = ?", gateway, externalID))
	if errors.Is(err, sql.ErrNoRows) {
		return p, core.ErrNotFound
	}
	return p, err
}

func (t *coreTx) SetPaymentExt(id int64, externalID string, extAmount int64, extCurrency string) error {
	_, err := t.q.ExecContext(t.ctx,
		"UPDATE payments SET external_id = ?, ext_amount = ?, ext_currency = ? WHERE id = ?",
		nullText(externalID), extAmount, extCurrency, id)
	return mapErr(err)
}

// MarkPaid это условное обновление в «оплачен». Поздняя оплата истёкшего платежа тоже зачисляется,
// а второй вызов и возвращённый платёж затронут 0 строк.
func (t *coreTx) MarkPaid(id int64, externalID string, at time.Time) (bool, error) {
	res, err := t.q.ExecContext(t.ctx,
		`UPDATE payments SET status = 'paid', paid_at = ?, external_id = COALESCE(?, external_id)
		 WHERE id = ? AND status IN ('pending', 'expired', 'failed')`, at.Unix(), nullText(externalID), id)
	if err != nil {
		return false, mapErr(err)
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

func (t *coreTx) SetPaymentStatus(id int64, from, to core.PayStatus) (bool, error) {
	res, err := t.q.ExecContext(t.ctx,
		"UPDATE payments SET status = ? WHERE id = ? AND status = ?", string(to), id, string(from))
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}
