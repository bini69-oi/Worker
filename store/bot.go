package store

import (
	"context"
	"database/sql"
	"errors"
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

func (t *coreTx) SetLang(userID int64, lang string) error {
	return t.execOne("UPDATE users SET lang = ? WHERE id = ?", lang, userID)
}

func (t *coreTx) AddSupportMsg(chatMsgID, userID int64, at time.Time) error {
	_, err := t.q.ExecContext(t.ctx,
		"INSERT INTO support_msgs (chat_msg_id, user_id, created_at) VALUES (?, ?, ?) ON CONFLICT (chat_msg_id) DO NOTHING",
		chatMsgID, userID, at.Unix())
	return mapErr(err)
}

func (t *coreTx) SupportMsg(chatMsgID int64) (int64, time.Time, error) {
	var userID, at int64
	err := t.q.QueryRowContext(t.ctx, "SELECT user_id, created_at FROM support_msgs WHERE chat_msg_id = ?", chatMsgID).Scan(&userID, &at)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, time.Time{}, core.ErrNotFound
	}
	return userID, time.Unix(at, 0).UTC(), err
}

// DeleteSupportBefore удаляет связи поддержки старше before.
func (s *CoreStore) DeleteSupportBefore(ctx context.Context, before time.Time) (int, error) {
	res, err := s.db.W.ExecContext(ctx, "DELETE FROM support_msgs WHERE created_at < ?", before.Unix())
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	return int(n), err
}
