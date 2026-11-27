package coretest

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/bini69-oi/worker/core"
)

// userRows собирает строки поиска из состояния: абонент и его подписка.
func (st *state) userRows() []core.UserRow {
	var out []core.UserRow
	for _, u := range st.users {
		row := core.UserRow{User: u}
		for _, sub := range st.subs {
			if sub.UserID == u.ID {
				row.HasSub, row.SubStatus, row.Trial, row.ExpiresAt = true, sub.Status, sub.Trial, sub.ExpiresAt
			}
		}
		out = append(out, row)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].User.ID > out[j].User.ID })
	return out
}

func (s *Store) FindUsers(_ context.Context, q core.UserQuery, now time.Time, limit int) ([]core.UserRow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []core.UserRow
	for _, r := range s.st.userRows() {
		u := r.User
		if len(out) >= limit || (q.After > 0 && u.ID >= q.After) {
			continue
		}
		byID := q.TgID != 0 || q.RefCode != ""
		if byID && !((q.TgID != 0 && u.TgID == q.TgID) || (q.RefCode != "" && u.RefCode == q.RefCode)) {
			continue
		}
		if q.UsernamePrefix != "" && !strings.HasPrefix(strings.ToLower(u.Username), strings.ToLower(q.UsernamePrefix)) {
			continue
		}
		if r.Matches(q.Filter, now) {
			out = append(out, r)
		}
	}
	return out, nil
}

func (s *Store) AuditLog(_ context.Context, f core.AuditFilter, limit int) ([]core.AuditRow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []core.AuditRow
	for i := len(s.st.audit) - 1; i >= 0 && len(out) < limit; i-- {
		a, id := s.st.audit[i], int64(i+1)
		switch {
		case f.After > 0 && id >= f.After, f.AdminTg != 0 && a.AdminTg != f.AdminTg,
			f.Action != "" && a.Action != f.Action, f.Target != "" && a.Target != f.Target,
			!f.From.IsZero() && a.At.Before(f.From), !f.To.IsZero() && !a.At.Before(f.To):
			continue
		}
		out = append(out, core.AuditRow{ID: id, AuditEntry: a})
	}
	return out, nil
}

func (s *Store) UserPayments(_ context.Context, userID int64, limit int) ([]core.Payment, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []core.Payment
	for _, p := range s.st.pays {
		if p.UserID == userID {
			out = append(out, p)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID > out[j].ID })
	return out[:min(len(out), limit)], nil
}
