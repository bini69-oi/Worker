package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/bini69-oi/worker/core"
)

// Enqueue кладёт задачу в очередь в той же транзакции, что и изменение, ради которого она появилась.
func (t *coreTx) Enqueue(kind string, payload []byte, at time.Time) (int64, error) {
	res, err := t.q.ExecContext(t.ctx,
		"INSERT INTO outbox (kind, payload, next_at, created_at) VALUES (?, ?, ?, ?)",
		kind, string(payload), at.Unix(), at.Unix())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// AddNotice отмечает уведомление. Если такое уже было, возвращает false и ничего не меняет.
func (t *coreTx) AddNotice(userID int64, kind, ref string, at time.Time) (bool, error) {
	_, err := t.q.ExecContext(t.ctx,
		"INSERT INTO notices (user_id, kind, ref, sent_at) VALUES (?, ?, ?, ?)", userID, kind, ref, at.Unix())
	if err == nil {
		return true, nil
	}
	if errors.Is(mapErr(err), core.ErrDuplicate) {
		return false, nil
	}
	return false, err
}

const taskCols = `id, kind, payload, attempts, created_at`

func scanTasks(rows *sql.Rows) ([]core.Task, error) {
	defer rows.Close()
	var out []core.Task
	for rows.Next() {
		var t core.Task
		var payload string
		var created int64
		if err := rows.Scan(&t.ID, &t.Kind, &payload, &t.Attempts, &created); err != nil {
			return nil, err
		}
		t.Payload = []byte(payload)
		t.CreatedAt = time.Unix(created, 0).UTC()
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *CoreStore) DueTasks(ctx context.Context, now time.Time, limit int) ([]core.Task, error) {
	rows, err := s.db.R.QueryContext(ctx,
		"SELECT "+taskCols+" FROM outbox WHERE done_at IS NULL AND next_at <= ? ORDER BY next_at, id LIMIT ?",
		now.Unix(), limit)
	if err != nil {
		return nil, err
	}
	return scanTasks(rows)
}

func (s *CoreStore) TaskDone(ctx context.Context, id int64, at time.Time) error {
	_, err := s.db.W.ExecContext(ctx, "UPDATE outbox SET done_at = ?, last_error = '' WHERE id = ?", at.Unix(), id)
	return err
}

func (s *CoreStore) TaskFailed(ctx context.Context, id int64, attempts int, next time.Time, lastErr string) error {
	_, err := s.db.W.ExecContext(ctx,
		"UPDATE outbox SET attempts = ?, next_at = ?, last_error = ? WHERE id = ?", attempts, next.Unix(), lastErr, id)
	return err
}

func (s *CoreStore) StuckTasks(ctx context.Context, before time.Time) ([]core.Task, error) {
	rows, err := s.db.R.QueryContext(ctx,
		"SELECT "+taskCols+` FROM outbox
		 WHERE done_at IS NULL AND alerted = 0 AND kind <> ? AND created_at < ? ORDER BY id`,
		core.KindNotifyAdmin, before.Unix())
	if err != nil {
		return nil, err
	}
	return scanTasks(rows)
}

func (s *CoreStore) SubsInPanel(ctx context.Context) ([]core.Subscription, error) {
	rows, err := s.db.R.QueryContext(ctx, "SELECT "+subCols+" FROM subscriptions WHERE panel_uuid <> '' ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []core.Subscription
	for rows.Next() {
		sub, err := scanSub(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sub)
	}
	return out, rows.Err()
}

func (t *coreTx) MarkAlerted(ids []int64) error {
	for _, id := range ids {
		if _, err := t.q.ExecContext(t.ctx, "UPDATE outbox SET alerted = 1 WHERE id = ?", id); err != nil {
			return err
		}
	}
	return nil
}
