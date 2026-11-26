package store

import (
	"time"

	"github.com/bini69-oi/worker/core"
)

// execOne выполняет изменение одной строки. Не нашлось строки: ErrNotFound.
func (t *coreTx) execOne(query string, args ...any) error {
	res, err := t.q.ExecContext(t.ctx, query, args...)
	if err != nil {
		return mapErr(err)
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return core.ErrNotFound
	}
	return nil
}

func (t *coreTx) SetUnreachable(userID int64, at time.Time) error {
	return t.execOne("UPDATE users SET unreachable_at = ? WHERE id = ?", nullTime(at), userID)
}

func (t *coreTx) SetScreenMsg(userID, msgID int64) error {
	return t.execOne("UPDATE users SET screen_msg_id = ? WHERE id = ?", msgID, userID)
}

func (t *coreTx) SetBanned(userID int64, at time.Time) error {
	return t.execOne("UPDATE users SET banned_at = ? WHERE id = ?", nullTime(at), userID)
}
