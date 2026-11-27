package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"time"

	"github.com/bini69-oi/worker/core"
)

// escapeLike экранирует знаки LIKE: имя пользователя `a%_` ищется как есть, а не как маска.
// Запрос должен нести ESCAPE '\'.
func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

// liveSQL это «подписка живая» для строки s из LEFT JOIN subscriptions.
const liveSQL = `(s.status = 'active' AND s.expires_at > ?)`

// filterSQL это условие фильтра абонентов. Правило то же, что у core.UserRow.Matches.
func filterSQL(filter string, now time.Time) (string, []any) {
	n := now.Unix()
	switch filter {
	case core.FilterActive:
		return liveSQL, []any{n}
	case core.FilterTrial:
		return liveSQL + " AND s.trial = 1", []any{n}
	case core.FilterExpired:
		return "s.id IS NOT NULL AND NOT " + liveSQL, []any{n}
	case core.SegExpired30:
		return "s.id IS NOT NULL AND NOT " + liveSQL + " AND s.expires_at > ?", []any{n, now.Add(-30 * 24 * time.Hour).Unix()}
	case core.FilterNone:
		return "s.id IS NULL", nil
	case core.FilterBanned:
		return "u.banned_at IS NOT NULL", nil
	}
	return "", nil
}

func scanUserRow(r scanner) (core.UserRow, error) {
	var row core.UserRow
	var referrer, banned, unreachable, subID, expires sql.NullInt64
	var status sql.NullString
	var trial sql.NullBool
	var created int64
	u := &row.User
	if err := r.Scan(&u.ID, &u.TgID, &u.Username, &u.Lang, &referrer, &u.RefCode, &u.Balance, &u.TrialUsed, &banned, &created,
		&unreachable, &u.ScreenMsgID, &subID, &status, &trial, &expires); err != nil {
		return row, err
	}
	u.ReferrerID = referrer.Int64
	u.BannedAt, u.UnreachableAt, u.CreatedAt = unix(banned), unix(unreachable), time.Unix(created, 0).UTC()
	row.HasSub = subID.Valid
	row.SubStatus, row.Trial, row.ExpiresAt = core.SubStatus(status.String), trial.Bool, unix(expires)
	return row, nil
}

const userRowCols = `u.id, u.tg_id, u.username, u.lang, u.referrer_id, u.ref_code, u.balance, u.trial_used, u.banned_at, u.created_at,
	u.unreachable_at, u.screen_msg_id, s.id, s.status, s.trial, s.expires_at`

func (s *CoreStore) FindUsers(ctx context.Context, q core.UserQuery, now time.Time, limit int) ([]core.UserRow, error) {
	where, args := []string{"1 = 1"}, []any{}
	if q.After > 0 {
		where, args = append(where, "u.id < ?"), append(args, q.After)
	}
	var or []string
	if q.TgID != 0 {
		or, args = append(or, "u.tg_id = ?"), append(args, q.TgID)
	}
	if q.RefCode != "" {
		or, args = append(or, "u.ref_code = ?"), append(args, q.RefCode)
	}
	if len(or) > 0 {
		where = append(where, "("+strings.Join(or, " OR ")+")")
	}
	if q.UsernamePrefix != "" {
		where, args = append(where, `u.username LIKE ? ESCAPE '\'`), append(args, escapeLike(q.UsernamePrefix)+"%")
	}
	if cond, a := filterSQL(q.Filter, now); cond != "" {
		where, args = append(where, cond), append(args, a...)
	}
	rows, err := s.db.R.QueryContext(ctx,
		"SELECT "+userRowCols+` FROM users u LEFT JOIN subscriptions s ON s.user_id = u.id WHERE `+
			strings.Join(where, " AND ")+" ORDER BY u.id DESC LIMIT ?", append(args, limit)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []core.UserRow
	for rows.Next() {
		row, err := scanUserRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (s *CoreStore) AuditLog(ctx context.Context, f core.AuditFilter, limit int) ([]core.AuditRow, error) {
	where, args := []string{"1 = 1"}, []any{}
	add := func(cond string, v any) { where, args = append(where, cond), append(args, v) }
	if f.After > 0 {
		add("id < ?", f.After)
	}
	if f.AdminTg != 0 {
		add("admin_tg_id = ?", f.AdminTg)
	}
	if f.Action != "" {
		add("action = ?", f.Action)
	}
	if f.Target != "" {
		add("target = ?", f.Target)
	}
	if !f.From.IsZero() {
		add("created_at >= ?", f.From.Unix())
	}
	if !f.To.IsZero() {
		add("created_at < ?", f.To.Unix())
	}
	rows, err := s.db.R.QueryContext(ctx,
		"SELECT id, admin_tg_id, action, target, details, ip, created_at FROM audit_log WHERE "+
			strings.Join(where, " AND ")+" ORDER BY id DESC LIMIT ?", append(args, limit)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []core.AuditRow
	for rows.Next() {
		var r core.AuditRow
		var details string
		var at int64
		if err := rows.Scan(&r.ID, &r.AdminTg, &r.Action, &r.Target, &details, &r.IP, &at); err != nil {
			return nil, err
		}
		if r.Details, err = parseDetails(details); err != nil {
			return nil, err
		}
		r.At = time.Unix(at, 0).UTC()
		out = append(out, r)
	}
	return out, rows.Err()
}

func parseDetails(raw string) (map[string]string, error) {
	var m map[string]string
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return nil, err
	}
	return m, nil
}
