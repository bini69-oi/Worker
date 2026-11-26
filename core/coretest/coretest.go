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
	pays    map[int64]core.Payment
	carts   map[int64]core.PendingBuy
	promos  map[int64]core.Promo
	limits  map[string]limitRow
	nextID  int64

	promoUses map[[2]int64]bool
	admins    map[int64]core.Admin
	audit     []core.AuditEntry
	confirms  map[int64]core.Confirm
	sessions  map[string]sessionRow
	chat      *core.AdminChat
	support   map[int64]supportRow
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
		pays:    maps.Clone(s.pays),
		carts:   maps.Clone(s.carts),
		promos:  maps.Clone(s.promos),
		limits:  maps.Clone(s.limits),

		promoUses: maps.Clone(s.promoUses),
		admins:    maps.Clone(s.admins),
		audit:     slices.Clone(s.audit),
		confirms:  maps.Clone(s.confirms),
		sessions:  maps.Clone(s.sessions),
		chat:      cloneChat(s.chat),
		support:   maps.Clone(s.support),
		nextID:    s.nextID,
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
		subs: map[int64]core.Subscription{}, notices: map[string]bool{}, pays: map[int64]core.Payment{},
		carts: map[int64]core.PendingBuy{}, promos: map[int64]core.Promo{}, limits: map[string]limitRow{},
		promoUses: map[[2]int64]bool{}, admins: map[int64]core.Admin{}, confirms: map[int64]core.Confirm{},
		sessions: map[string]sessionRow{},
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
	if _, err := t.UserByRef(u.RefCode); err == nil || u.RefCode == "" {
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

func (t *tx) SetUnreachable(userID int64, at time.Time) error {
	u, ok := t.st.users[userID]
	if !ok {
		return core.ErrNotFound
	}
	u.UnreachableAt = at
	t.st.users[userID] = u
	return nil
}

func (t *tx) SetScreenMsg(userID, msgID int64) error {
	u, ok := t.st.users[userID]
	if !ok {
		return core.ErrNotFound
	}
	u.ScreenMsgID = msgID
	t.st.users[userID] = u
	return nil
}

func (t *tx) SetBanned(userID int64, at time.Time) error {
	u, ok := t.st.users[userID]
	if !ok {
		return core.ErrNotFound
	}
	u.BannedAt = at
	t.st.users[userID] = u
	return nil
}

func (t *tx) SetLang(userID int64, lang string) error {
	u, ok := t.st.users[userID]
	if !ok {
		return core.ErrNotFound
	}
	u.Lang = lang
	t.st.users[userID] = u
	return nil
}

type supportRow struct {
	user int64
	at   time.Time
}

func (t *tx) AddSupportMsg(chatMsgID, userID int64, at time.Time) error {
	if t.st.support == nil {
		t.st.support = map[int64]supportRow{}
	}
	if _, ok := t.st.support[chatMsgID]; !ok {
		t.st.support[chatMsgID] = supportRow{user: userID, at: at}
	}
	return nil
}

func (t *tx) SupportMsg(chatMsgID int64) (int64, time.Time, error) {
	r, ok := t.st.support[chatMsgID]
	if !ok {
		return 0, time.Time{}, core.ErrNotFound
	}
	return r.user, r.at, nil
}

// DeleteSupportBefore удаляет связи поддержки старше before.
func (s *Store) DeleteSupportBefore(_ context.Context, before time.Time) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for id, r := range s.st.support {
		if r.at.Before(before) {
			delete(s.st.support, id)
			n++
		}
	}
	return n, nil
}
