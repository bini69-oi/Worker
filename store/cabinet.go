package store

import (
	"context"
	"database/sql"
	"encoding/hex"
	"errors"
	"time"

	"github.com/bini69-oi/worker/core"
)

const userSessionCols = `token_hash, user_id, kind, ip, agent, created_at, seen_at, expires_at, closed_at`

func scanUserSession(r scanner) (core.UserSession, error) {
	var s core.UserSession
	var hash []byte
	var kind string
	var created, seen, expires int64
	var closed sql.NullInt64
	if err := r.Scan(&hash, &s.UserID, &kind, &s.IP, &s.Agent, &created, &seen, &expires, &closed); err != nil {
		return s, err
	}
	s.ID, s.Kind = hex.EncodeToString(hash[:min(8, len(hash))]), core.SessionKind(kind)
	s.CreatedAt, s.SeenAt, s.ExpiresAt, s.ClosedAt = time.Unix(created, 0).UTC(), time.Unix(seen, 0).UTC(), time.Unix(expires, 0).UTC(), unix(closed)
	return s, nil
}

func (t *coreTx) CreateUserSession(hash []byte, s core.UserSession) error {
	_, err := t.q.ExecContext(t.ctx,
		"INSERT INTO user_sessions ("+userSessionCols+") VALUES (?, ?, ?, ?, ?, ?, ?, ?, NULL)",
		hash, s.UserID, string(s.Kind), s.IP, s.Agent, s.CreatedAt.Unix(), s.SeenAt.Unix(), s.ExpiresAt.Unix())
	return err
}

func (t *coreTx) UserSession(hash []byte) (core.UserSession, error) {
	s, err := scanUserSession(t.q.QueryRowContext(t.ctx, "SELECT "+userSessionCols+" FROM user_sessions WHERE token_hash = ?", hash))
	if errors.Is(err, sql.ErrNoRows) {
		return s, core.ErrNotFound
	}
	return s, err
}

func (t *coreTx) TouchUserSession(hash []byte, at time.Time) error {
	_, err := t.q.ExecContext(t.ctx, "UPDATE user_sessions SET seen_at = ? WHERE token_hash = ?", at.Unix(), hash)
	return err
}

func (t *coreTx) CloseUserSession(hash []byte, at time.Time) error {
	_, err := t.q.ExecContext(t.ctx, "UPDATE user_sessions SET closed_at = ? WHERE token_hash = ? AND closed_at IS NULL", at.Unix(), hash)
	return err
}

func (t *coreTx) CloseUserSessions(userID int64, at time.Time) error {
	_, err := t.q.ExecContext(t.ctx, "UPDATE user_sessions SET closed_at = ? WHERE user_id = ? AND closed_at IS NULL", at.Unix(), userID)
	return err
}

const userLoginCols = `id, id_hash, nonce_hash, code, tg_id, ip, agent, created_at, expires_at, answer, answered_at, used_at`

func scanUserLogin(r scanner) (core.UserLogin, error) {
	var l core.UserLogin
	var created, expires int64
	var answered, used sql.NullInt64
	if err := r.Scan(&l.ID, &l.IDHash, &l.NonceHash, &l.Code, &l.TgID, &l.IP, &l.Agent, &created, &expires, &l.Answer, &answered, &used); err != nil {
		return l, err
	}
	l.CreatedAt, l.ExpiresAt, l.AnsweredAt, l.UsedAt = time.Unix(created, 0).UTC(), time.Unix(expires, 0).UTC(), unix(answered), unix(used)
	return l, nil
}

func (t *coreTx) userLogin(where string, arg any) (core.UserLogin, error) {
	l, err := scanUserLogin(t.q.QueryRowContext(t.ctx, "SELECT "+userLoginCols+" FROM user_logins WHERE "+where, arg))
	if errors.Is(err, sql.ErrNoRows) {
		return l, core.ErrNotFound
	}
	return l, err
}

func (t *coreTx) UserLogin(id int64) (core.UserLogin, error) { return t.userLogin("id = ?", id) }

func (t *coreTx) UserLoginByHash(h []byte) (core.UserLogin, error) {
	return t.userLogin("id_hash = ?", h)
}

func (t *coreTx) UserLoginByNonce(h []byte) (core.UserLogin, error) {
	return t.userLogin("nonce_hash = ?", h)
}

func (t *coreTx) CreateUserLogin(l core.UserLogin) (int64, error) {
	res, err := t.q.ExecContext(t.ctx,
		`INSERT INTO user_logins (id_hash, nonce_hash, code, tg_id, ip, agent, created_at, expires_at, answer)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, '')`,
		l.IDHash, l.NonceHash, l.Code, l.TgID, l.IP, l.Agent, l.CreatedAt.Unix(), l.ExpiresAt.Unix())
	if err != nil {
		return 0, mapErr(err)
	}
	return res.LastInsertId()
}

func (t *coreTx) SaveUserLogin(l core.UserLogin) error {
	_, err := t.q.ExecContext(t.ctx, "UPDATE user_logins SET tg_id = ?, answer = ?, answered_at = ?, used_at = ? WHERE id = ?",
		l.TgID, l.Answer, nullTime(l.AnsweredAt), nullTime(l.UsedAt), l.ID)
	return err
}

func (t *coreTx) PruneCabinet(before time.Time) error {
	if _, err := t.q.ExecContext(t.ctx,
		"DELETE FROM user_sessions WHERE expires_at < ? OR closed_at < ?", before.Unix(), before.Unix()); err != nil {
		return err
	}
	if _, err := t.q.ExecContext(t.ctx, "DELETE FROM user_logins WHERE expires_at < ?", before.Unix()); err != nil {
		return err
	}
	// Отметки ключей повтора покупок нужны на время, пока страница может повторить запрос.
	_, err := t.q.ExecContext(t.ctx, `DELETE FROM notices WHERE kind LIKE 'req\_%' ESCAPE '\' AND sent_at < ?`, before.Unix())
	return err
}

// UserSession читает сессию с читающего соединения.
func (s *CoreStore) UserSession(ctx context.Context, hash []byte) (core.UserSession, error) {
	return (&coreTx{ctx: ctx, q: s.db.R}).UserSession(hash)
}

func (t *coreTx) HasNotice(userID int64, kind, ref string) (bool, error) {
	var one int
	err := t.q.QueryRowContext(t.ctx, "SELECT 1 FROM notices WHERE user_id = ? AND kind = ? AND ref = ?", userID, kind, ref).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}
