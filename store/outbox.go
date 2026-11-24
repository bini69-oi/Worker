package store

import (
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
