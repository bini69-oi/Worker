package store

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/bini69-oi/worker/core"
)

func unix(v sql.NullInt64) time.Time {
	if !v.Valid {
		return time.Time{}
	}
	return time.Unix(v.Int64, 0).UTC()
}

const adminCols = `tg_id, pass_hash, perms, created_at, disabled_at`

func scanAdmin(r scanner) (core.Admin, error) {
	var a core.Admin
	var perms string
	var created int64
	var disabled sql.NullInt64
	if err := r.Scan(&a.TgID, &a.PassHash, &perms, &created, &disabled); err != nil {
		return a, err
	}
	if err := json.Unmarshal([]byte(perms), &a.Perms); err != nil {
		return a, err
	}
	a.CreatedAt, a.DisabledAt = time.Unix(created, 0).UTC(), unix(disabled)
	return a, nil
}

func (s *CoreStore) Admins(ctx context.Context) ([]core.Admin, error) {
	rows, err := s.db.R.QueryContext(ctx, "SELECT "+adminCols+" FROM admins ORDER BY tg_id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []core.Admin
	for rows.Next() {
		a, err := scanAdmin(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (t *coreTx) Admin(tgID int64) (core.Admin, error) {
	a, err := scanAdmin(t.q.QueryRowContext(t.ctx, "SELECT "+adminCols+" FROM admins WHERE tg_id = ?", tgID))
	if errors.Is(err, sql.ErrNoRows) {
		return a, core.ErrNotFound
	}
	return a, err
}

func (t *coreTx) SaveAdmin(a core.Admin) error {
	perms, err := json.Marshal(a.Perms)
	if err != nil {
		return err
	}
	if a.Perms == nil {
		perms = []byte("[]")
	}
	_, err = t.q.ExecContext(t.ctx,
		`INSERT INTO admins (tg_id, pass_hash, perms, created_at, disabled_at) VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT (tg_id) DO UPDATE SET pass_hash = excluded.pass_hash, perms = excluded.perms, disabled_at = excluded.disabled_at`,
		a.TgID, a.PassHash, string(perms), a.CreatedAt.Unix(), nullTime(a.DisabledAt))
	return err
}

// AddAudit пишет строку журнала. Правку и удаление журнала запрещают триггеры в схеме.
func (t *coreTx) AddAudit(a core.AuditEntry) error {
	details, err := json.Marshal(a.Details)
	if err != nil {
		return err
	}
	if a.Details == nil {
		details = []byte("{}")
	}
	_, err = t.q.ExecContext(t.ctx,
		"INSERT INTO audit_log (admin_tg_id, action, target, details, ip, created_at) VALUES (?, ?, ?, ?, ?, ?)",
		a.AdminTg, a.Action, a.Target, string(details), a.IP, a.At.Unix())
	return err
}

const confirmCols = `id, admin_tg_id, kind, payload, created_at, expires_at, answer, answered_at, used_at`

func (t *coreTx) CreateConfirm(cf core.Confirm) (int64, error) {
	res, err := t.q.ExecContext(t.ctx,
		"INSERT INTO confirms (admin_tg_id, kind, payload, created_at, expires_at) VALUES (?, ?, ?, ?, ?)",
		cf.AdminTg, cf.Kind, string(cf.Payload), cf.CreatedAt.Unix(), cf.ExpiresAt.Unix())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (t *coreTx) Confirm(id int64) (core.Confirm, error) {
	var cf core.Confirm
	var payload string
	var answer sql.NullString
	var created, expires int64
	var answered, used sql.NullInt64
	err := t.q.QueryRowContext(t.ctx, "SELECT "+confirmCols+" FROM confirms WHERE id = ?", id).
		Scan(&cf.ID, &cf.AdminTg, &cf.Kind, &payload, &created, &expires, &answer, &answered, &used)
	if errors.Is(err, sql.ErrNoRows) {
		return cf, core.ErrNotFound
	}
	if err != nil {
		return cf, err
	}
	cf.Payload, cf.Answer = []byte(payload), answer.String
	cf.CreatedAt, cf.ExpiresAt = time.Unix(created, 0).UTC(), time.Unix(expires, 0).UTC()
	cf.AnsweredAt, cf.UsedAt = unix(answered), unix(used)
	return cf, nil
}

// AnswerConfirm это условное обновление: второе нажатие и нажатие после срока не меняют ничего.
func (t *coreTx) AnswerConfirm(id int64, answer string, at time.Time) (bool, error) {
	res, err := t.q.ExecContext(t.ctx,
		"UPDATE confirms SET answer = ?, answered_at = ? WHERE id = ? AND answer IS NULL AND expires_at > ?",
		answer, at.Unix(), id, at.Unix())
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

func (t *coreTx) UseConfirm(id int64, at time.Time) (bool, error) {
	res, err := t.q.ExecContext(t.ctx,
		"UPDATE confirms SET used_at = ? WHERE id = ? AND answer = 'yes' AND used_at IS NULL", at.Unix(), id)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

const sessionCols = `token_hash, admin_tg_id, ip, agent, created_at, expires_at, closed_at`

func scanSession(r scanner) (core.Session, error) {
	var s core.Session
	var hash []byte
	var created, expires int64
	var closed sql.NullInt64
	if err := r.Scan(&hash, &s.AdminTg, &s.IP, &s.Agent, &created, &expires, &closed); err != nil {
		return s, err
	}
	s.ID = hex.EncodeToString(hash[:min(8, len(hash))])
	s.CreatedAt, s.ExpiresAt, s.ClosedAt = time.Unix(created, 0).UTC(), time.Unix(expires, 0).UTC(), unix(closed)
	return s, nil
}

func (s *CoreStore) Sessions(ctx context.Context, now time.Time) ([]core.Session, error) {
	rows, err := s.db.R.QueryContext(ctx,
		"SELECT "+sessionCols+" FROM admin_sessions WHERE closed_at IS NULL AND expires_at > ? ORDER BY created_at", now.Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []core.Session
	for rows.Next() {
		sess, err := scanSession(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sess)
	}
	return out, rows.Err()
}

func (t *coreTx) CreateSession(hash []byte, s core.Session) error {
	_, err := t.q.ExecContext(t.ctx,
		"INSERT INTO admin_sessions (token_hash, admin_tg_id, ip, agent, created_at, expires_at) VALUES (?, ?, ?, ?, ?, ?)",
		hash, s.AdminTg, s.IP, s.Agent, s.CreatedAt.Unix(), s.ExpiresAt.Unix())
	return err
}

func (t *coreTx) Session(hash []byte) (core.Session, error) {
	s, err := scanSession(t.q.QueryRowContext(t.ctx, "SELECT "+sessionCols+" FROM admin_sessions WHERE token_hash = ?", hash))
	if errors.Is(err, sql.ErrNoRows) {
		return s, core.ErrNotFound
	}
	return s, err
}

// CloseSession закрывает сессию по её короткому номеру (начало хэша в hex).
func (t *coreTx) CloseSession(id string, at time.Time) error {
	prefix, err := hex.DecodeString(id)
	if err != nil || len(prefix) == 0 {
		return core.ErrInvalid
	}
	_, err = t.q.ExecContext(t.ctx,
		"UPDATE admin_sessions SET closed_at = ? WHERE substr(token_hash, 1, ?) = ? AND closed_at IS NULL",
		at.Unix(), len(prefix), prefix)
	return err
}

func (t *coreTx) CloseAdminSessions(adminTg int64, at time.Time) error {
	_, err := t.q.ExecContext(t.ctx,
		"UPDATE admin_sessions SET closed_at = ? WHERE admin_tg_id = ? AND closed_at IS NULL", at.Unix(), adminTg)
	return err
}

func (t *coreTx) AdminChat() (core.AdminChat, error) {
	var ch core.AdminChat
	var chat, bound sql.NullInt64
	var topics string
	err := t.q.QueryRowContext(t.ctx, "SELECT chat_id, bind_code_hash, topics, bound_at FROM admin_chat WHERE id = 1").
		Scan(&chat, &ch.CodeHash, &topics, &bound)
	if errors.Is(err, sql.ErrNoRows) {
		return ch, core.ErrNotFound
	}
	if err != nil {
		return ch, err
	}
	ch.ChatID, ch.BoundAt = chat.Int64, unix(bound)
	err = json.Unmarshal([]byte(topics), &ch.Topics)
	return ch, err
}

func (t *coreTx) SaveAdminChat(ch core.AdminChat) error {
	topics, err := json.Marshal(ch.Topics)
	if err != nil {
		return err
	}
	if ch.Topics == nil {
		topics = []byte("{}")
	}
	_, err = t.q.ExecContext(t.ctx,
		`INSERT INTO admin_chat (id, chat_id, bind_code_hash, topics, bound_at) VALUES (1, ?, ?, ?, ?)
		 ON CONFLICT (id) DO UPDATE SET chat_id = excluded.chat_id, bind_code_hash = excluded.bind_code_hash,
			topics = excluded.topics, bound_at = excluded.bound_at`,
		nullID(ch.ChatID), ch.CodeHash, string(topics), nullTime(ch.BoundAt))
	return err
}
