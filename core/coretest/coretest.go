// Package coretest это фейки для тестов ядра: хранилище в памяти и часы, которые двигают руками.
// Фейк ведёт себя как настоящая база: транзакция либо целиком, либо никак, баланс не уходит в минус,
// одно событие двигает деньги один раз.
package coretest

import (
	"context"
	"maps"
	"slices"
	"sync"
	"time"

	"github.com/bini69-oi/worker/core"
)

// Clock это часы для тестов.
type Clock struct {
	mu sync.Mutex
	t  time.Time
}

// NewClock ставит часы на заданное время.
func NewClock(t time.Time) *Clock { return &Clock{t: t} }

func (c *Clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

// Add двигает часы вперёд.
func (c *Clock) Add(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

type refKey struct {
	typ string
	id  int64
}

// taskRow это строка очереди: задача и её служебные поля.
type taskRow struct {
	core.Task
	nextAt  time.Time
	lastErr string
	done    bool
	alerted bool
}

type state struct {
	users   map[int64]core.User
	ledger  []core.LedgerEntry
	refs    map[refKey]bool
	tariffs map[int64]core.Tariff
	subs    map[int64]core.Subscription
	tasks   []taskRow
	notices map[string]bool
	nextID  int64
}

func (s *state) clone() *state {
	return &state{
		users:   maps.Clone(s.users),
		ledger:  slices.Clone(s.ledger),
		refs:    maps.Clone(s.refs),
		tariffs: maps.Clone(s.tariffs),
		subs:    maps.Clone(s.subs),
		tasks:   slices.Clone(s.tasks),
		notices: maps.Clone(s.notices),
		nextID:  s.nextID,
	}
}

func (s *state) id() int64 {
	s.nextID++
	return s.nextID
}

// Store это хранилище ядра в памяти. Транзакции идут по одной, как у SQLite.
type Store struct {
	mu sync.Mutex
	st *state
}

// NewStore возвращает пустое хранилище.
func NewStore() *Store {
	return &Store{st: &state{
		users: map[int64]core.User{}, refs: map[refKey]bool{}, tariffs: map[int64]core.Tariff{},
		subs: map[int64]core.Subscription{}, notices: map[string]bool{},
	}}
}

// Tx работает на копии состояния и заменяет им настоящее, только если fn вернула nil.
func (s *Store) Tx(_ context.Context, fn func(core.Tx) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	work := s.st.clone()
	if err := fn(&tx{st: work}); err != nil {
		return err
	}
	s.st = work
	return nil
}

func (s *Store) User(_ context.Context, id int64) (core.User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return (&tx{st: s.st}).User(id)
}

func (s *Store) UserByTg(_ context.Context, tgID int64) (core.User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return (&tx{st: s.st}).UserByTg(tgID)
}

func (s *Store) Ledger(_ context.Context, userID int64, limit int) ([]core.LedgerEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []core.LedgerEntry
	for i := len(s.st.ledger) - 1; i >= 0 && len(out) < limit; i-- {
		if s.st.ledger[i].UserID == userID {
			out = append(out, s.st.ledger[i])
		}
	}
	return out, nil
}

type tx struct{ st *state }

func (t *tx) User(id int64) (core.User, error) {
	u, ok := t.st.users[id]
	if !ok {
		return core.User{}, core.ErrNotFound
	}
	return u, nil
}

func (t *tx) UserByTg(tgID int64) (core.User, error) {
	for _, u := range t.st.users {
		if u.TgID == tgID {
			return u, nil
		}
	}
	return core.User{}, core.ErrNotFound
}

func (t *tx) CreateUser(u core.User) (int64, error) {
	if _, err := t.UserByTg(u.TgID); err == nil {
		return 0, core.ErrDuplicate
	}
	u.ID = t.st.id()
	t.st.users[u.ID] = u
	return u.ID, nil
}

func (t *tx) AddLedger(e core.LedgerEntry) (int64, error) {
	u, ok := t.st.users[e.UserID]
	if !ok {
		return 0, core.ErrNotFound
	}
	if e.RefID != 0 {
		k := refKey{e.RefType, e.RefID}
		if t.st.refs[k] {
			return 0, core.ErrDuplicate
		}
		t.st.refs[k] = true
	}
	if u.Balance+e.Amount < 0 {
		return 0, core.ErrNotEnoughMoney
	}
	u.Balance += e.Amount
	t.st.users[u.ID] = u
	e.ID = t.st.id()
	t.st.ledger = append(t.st.ledger, e)
	return e.ID, nil
}
