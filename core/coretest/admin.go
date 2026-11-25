package coretest

import (
	"context"
	"encoding/hex"
	"maps"
	"slices"
	"sort"
	"time"

	"github.com/bini69-oi/worker/core"
)

type sessionRow struct {
	core.Session
	hash string
}

func cloneChat(ch *core.AdminChat) *core.AdminChat {
	if ch == nil {
		return nil
	}
	c := *ch
	c.CodeHash = slices.Clone(ch.CodeHash)
	c.Topics = maps.Clone(ch.Topics)
	return &c
}

// Audit отдаёт журнал действий админов.
func (s *Store) Audit() []core.AuditEntry {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.st.audit)
}

func (s *Store) Admins(_ context.Context) ([]core.Admin, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []core.Admin
	for _, a := range s.st.admins {
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].TgID < out[j].TgID })
	return out, nil
}

func (s *Store) Sessions(_ context.Context, now time.Time) ([]core.Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []core.Session
	for _, r := range s.st.sessions {
		if r.ClosedAt.IsZero() && now.Before(r.ExpiresAt) {
			out = append(out, r.Session)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}

func (t *tx) Admin(tgID int64) (core.Admin, error) {
	a, ok := t.st.admins[tgID]
	if !ok {
		return a, core.ErrNotFound
	}
	return a, nil
}

func (t *tx) SaveAdmin(a core.Admin) error {
	a.Perms = slices.Clone(a.Perms)
	t.st.admins[a.TgID] = a
	return nil
}

func (t *tx) AddAudit(a core.AuditEntry) error {
	a.Details = maps.Clone(a.Details)
	t.st.audit = append(t.st.audit, a)
	return nil
}

func (t *tx) CreateConfirm(cf core.Confirm) (int64, error) {
	cf.ID = t.st.id()
	t.st.confirms[cf.ID] = cf
	return cf.ID, nil
}

func (t *tx) Confirm(id int64) (core.Confirm, error) {
	cf, ok := t.st.confirms[id]
	if !ok {
		return cf, core.ErrNotFound
	}
	return cf, nil
}

func (t *tx) AnswerConfirm(id int64, answer string, at time.Time) (bool, error) {
	cf, ok := t.st.confirms[id]
	if !ok || cf.Answer != "" || !at.Before(cf.ExpiresAt) {
		return false, nil
	}
	cf.Answer, cf.AnsweredAt = answer, at
	t.st.confirms[id] = cf
	return true, nil
}

func (t *tx) UseConfirm(id int64, at time.Time) (bool, error) {
	cf, ok := t.st.confirms[id]
	if !ok || cf.Answer != "yes" || !cf.UsedAt.IsZero() {
		return false, nil
	}
	cf.UsedAt = at
	t.st.confirms[id] = cf
	return true, nil
}

func (t *tx) CreateSession(hash []byte, s core.Session) error {
	t.st.sessions[hex.EncodeToString(hash)] = sessionRow{Session: s, hash: hex.EncodeToString(hash)}
	return nil
}

func (t *tx) Session(hash []byte) (core.Session, error) {
	r, ok := t.st.sessions[hex.EncodeToString(hash)]
	if !ok {
		return core.Session{}, core.ErrNotFound
	}
	return r.Session, nil
}

func (t *tx) TouchSession(hash []byte, at time.Time) error {
	k := hex.EncodeToString(hash)
	if r, ok := t.st.sessions[k]; ok {
		r.SeenAt = at
		t.st.sessions[k] = r
	}
	return nil
}

func (t *tx) CloseSession(id string, at time.Time) error {
	for k, r := range t.st.sessions {
		if r.ID == id && r.ClosedAt.IsZero() {
			r.ClosedAt = at
			t.st.sessions[k] = r
		}
	}
	return nil
}

func (t *tx) CloseAdminSessions(adminTg int64, at time.Time) error {
	for k, r := range t.st.sessions {
		if r.AdminTg == adminTg && r.ClosedAt.IsZero() {
			r.ClosedAt = at
			t.st.sessions[k] = r
		}
	}
	return nil
}

func (t *tx) AdminChat() (core.AdminChat, error) {
	if t.st.chat == nil {
		return core.AdminChat{}, core.ErrNotFound
	}
	return *cloneChat(t.st.chat), nil
}

func (t *tx) SaveAdminChat(ch core.AdminChat) error {
	t.st.chat = cloneChat(&ch)
	return nil
}
