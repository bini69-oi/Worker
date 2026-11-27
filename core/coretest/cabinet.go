package coretest

import (
	"context"
	"encoding/hex"
	"maps"
	"time"

	"github.com/bini69-oi/worker/core"
)

// cabState это сессии абонентов и запросы входа в браузере.
type cabState struct {
	sessions map[string]core.UserSession // по хэшу токена в hex
	logins   map[int64]core.UserLogin
}

func (c cabState) clone() cabState {
	return cabState{sessions: maps.Clone(c.sessions), logins: maps.Clone(c.logins)}
}

func (t *tx) CreateUserSession(hash []byte, s core.UserSession) error {
	if t.st.cab.sessions == nil {
		t.st.cab.sessions = map[string]core.UserSession{}
	}
	t.st.cab.sessions[hex.EncodeToString(hash)] = s
	return nil
}

func (t *tx) UserSession(hash []byte) (core.UserSession, error) {
	s, ok := t.st.cab.sessions[hex.EncodeToString(hash)]
	if !ok {
		return s, core.ErrNotFound
	}
	return s, nil
}

func (t *tx) TouchUserSession(hash []byte, at time.Time) error {
	k := hex.EncodeToString(hash)
	if s, ok := t.st.cab.sessions[k]; ok {
		s.SeenAt = at
		t.st.cab.sessions[k] = s
	}
	return nil
}

func (t *tx) CloseUserSession(hash []byte, at time.Time) error {
	k := hex.EncodeToString(hash)
	if s, ok := t.st.cab.sessions[k]; ok && s.ClosedAt.IsZero() {
		s.ClosedAt = at
		t.st.cab.sessions[k] = s
	}
	return nil
}

func (t *tx) CloseUserSessions(userID int64, at time.Time) error {
	for k, s := range t.st.cab.sessions {
		if s.UserID == userID && s.ClosedAt.IsZero() {
			s.ClosedAt = at
			t.st.cab.sessions[k] = s
		}
	}
	return nil
}

func (t *tx) CreateUserLogin(l core.UserLogin) (int64, error) {
	if t.st.cab.logins == nil {
		t.st.cab.logins = map[int64]core.UserLogin{}
	}
	l.ID = t.st.id()
	t.st.cab.logins[l.ID] = l
	return l.ID, nil
}

func (t *tx) UserLogin(id int64) (core.UserLogin, error) {
	l, ok := t.st.cab.logins[id]
	if !ok {
		return l, core.ErrNotFound
	}
	return l, nil
}

func (t *tx) findLogin(match func(core.UserLogin) bool) (core.UserLogin, error) {
	for _, l := range t.st.cab.logins {
		if match(l) {
			return l, nil
		}
	}
	return core.UserLogin{}, core.ErrNotFound
}

func (t *tx) UserLoginByHash(h []byte) (core.UserLogin, error) {
	return t.findLogin(func(l core.UserLogin) bool { return string(l.IDHash) == string(h) })
}

func (t *tx) UserLoginByNonce(h []byte) (core.UserLogin, error) {
	return t.findLogin(func(l core.UserLogin) bool { return string(l.NonceHash) == string(h) })
}

func (t *tx) SaveUserLogin(l core.UserLogin) error {
	if _, ok := t.st.cab.logins[l.ID]; !ok {
		return core.ErrNotFound
	}
	t.st.cab.logins[l.ID] = l
	return nil
}

func (t *tx) PruneCabinet(before time.Time) error {
	for k, s := range t.st.cab.sessions {
		if s.ExpiresAt.Before(before) || (!s.ClosedAt.IsZero() && s.ClosedAt.Before(before)) {
			delete(t.st.cab.sessions, k)
		}
	}
	for id, l := range t.st.cab.logins {
		if l.ExpiresAt.Before(before) {
			delete(t.st.cab.logins, id)
		}
	}
	return nil
}

// UserSession читает сессию абонента вне транзакции.
func (s *Store) UserSession(_ context.Context, hash []byte) (core.UserSession, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return (&tx{st: s.st}).UserSession(hash)
}
