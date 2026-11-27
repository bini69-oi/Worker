package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/bini69-oi/worker/core"
)

const bcCols = `id, segment, text, button_text, button_url, status, total, sent, failed, blocked, cursor_user_id,
	created_by, created_at, started_at, done_at`

func scanBroadcast(r scanner) (core.Broadcast, error) {
	var b core.Broadcast
	var created int64
	var started, done sql.NullInt64
	err := r.Scan(&b.ID, &b.Segment, &b.Text, &b.ButtonText, &b.ButtonURL, &b.Status, &b.Total, &b.Sent, &b.Failed, &b.Blocked,
		&b.CursorUserID, &b.CreatedBy, &created, &started, &done)
	if errors.Is(err, sql.ErrNoRows) {
		return b, core.ErrNotFound
	}
	b.CreatedAt, b.StartedAt, b.DoneAt = time.Unix(created, 0).UTC(), unix(started), unix(done)
	return b, err
}

func (t *coreTx) CreateBroadcast(b core.Broadcast) (int64, error) {
	res, err := t.q.ExecContext(t.ctx,
		`INSERT INTO broadcasts (segment, text, button_text, button_url, status, created_by, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		b.Segment, b.Text, b.ButtonText, b.ButtonURL, b.Status, b.CreatedBy, b.CreatedAt.Unix())
	if err != nil {
		return 0, mapErr(err)
	}
	return res.LastInsertId()
}

func (t *coreTx) Broadcast(id int64) (core.Broadcast, error) {
	return scanBroadcast(t.q.QueryRowContext(t.ctx, "SELECT "+bcCols+" FROM broadcasts WHERE id = ?", id))
}

func (t *coreTx) RunningBroadcast() (core.Broadcast, error) {
	return scanBroadcast(t.q.QueryRowContext(t.ctx, "SELECT "+bcCols+" FROM broadcasts WHERE status = 'running'"))
}

// changed говорит, затронула ли запись ровно одну строку.
func changed(res sql.Result, err error) (bool, error) {
	if err != nil {
		return false, mapErr(err)
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

func (t *coreTx) StartBroadcast(id int64, total int, at time.Time) (bool, error) {
	return changed(t.q.ExecContext(t.ctx,
		"UPDATE broadcasts SET status = 'running', total = ?, started_at = ? WHERE id = ? AND status = 'draft'", total, at.Unix(), id))
}

func (t *coreTx) CancelBroadcast(id int64, at time.Time) (bool, error) {
	return changed(t.q.ExecContext(t.ctx,
		"UPDATE broadcasts SET status = 'cancelled', done_at = ? WHERE id = ? AND status IN ('draft', 'running')", at.Unix(), id))
}

func (t *coreTx) BroadcastStep(id, userID int64, sent, failed, blocked int) (bool, error) {
	ok, err := changed(t.q.ExecContext(t.ctx,
		`UPDATE broadcasts SET cursor_user_id = ?, sent = sent + ?, failed = failed + ?, blocked = blocked + ?
		 WHERE id = ? AND status IN ('running', 'cancelled')`, userID, sent, failed, blocked, id))
	if err != nil || !ok {
		return false, err
	}
	var status string
	if err := t.q.QueryRowContext(t.ctx, "SELECT status FROM broadcasts WHERE id = ?", id).Scan(&status); err != nil {
		return false, err
	}
	return status == core.BcRunning, nil
}

func (t *coreTx) FinishBroadcast(id int64, at time.Time) (bool, error) {
	return changed(t.q.ExecContext(t.ctx,
		"UPDATE broadcasts SET status = 'done', done_at = ? WHERE id = ? AND status = 'running'", at.Unix(), id))
}

// segmentWhere это получатели сегмента: без забаненных и недоступных плюс условие сегмента.
func segmentWhere(segment string, now time.Time) (string, []any) {
	where := "u.banned_at IS NULL AND u.unreachable_at IS NULL"
	if segment == core.SegAll {
		return where, nil
	}
	cond, args := filterSQL(segment, now)
	return where + " AND " + cond, args
}

const segmentFrom = " FROM users u LEFT JOIN subscriptions s ON s.user_id = u.id WHERE "

func (t *coreTx) SegmentCount(segment string, now time.Time) (int, error) {
	where, args := segmentWhere(segment, now)
	var n int
	err := t.q.QueryRowContext(t.ctx, "SELECT COUNT(*)"+segmentFrom+where, args...).Scan(&n)
	return n, err
}

func (s *CoreStore) SegmentCount(ctx context.Context, segment string, now time.Time) (int, error) {
	return (&coreTx{ctx: ctx, q: s.db.R}).SegmentCount(segment, now)
}

func (s *CoreStore) SegmentUsers(ctx context.Context, segment string, after int64, now time.Time, limit int) ([]core.User, error) {
	where, args := segmentWhere(segment, now)
	rows, err := s.db.R.QueryContext(ctx,
		"SELECT "+userRowCols+segmentFrom+where+" AND u.id > ? ORDER BY u.id LIMIT ?", append(args, after, limit)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []core.User
	for rows.Next() {
		r, err := scanUserRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r.User)
	}
	return out, rows.Err()
}

func (s *CoreStore) Broadcasts(ctx context.Context, after int64, limit int) ([]core.Broadcast, error) {
	q, args := "SELECT "+bcCols+" FROM broadcasts", []any{}
	if after > 0 {
		q, args = q+" WHERE id < ?", append(args, after)
	}
	rows, err := s.db.R.QueryContext(ctx, q+" ORDER BY id DESC LIMIT ?", append(args, limit)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []core.Broadcast
	for rows.Next() {
		b, err := scanBroadcast(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

func (s *CoreStore) BroadcastByID(ctx context.Context, id int64) (core.Broadcast, error) {
	return (&coreTx{ctx: ctx, q: s.db.R}).Broadcast(id)
}
