package coretest

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"time"

	"github.com/bini69-oi/worker/core"
)

func cloneTariff(t core.Tariff) core.Tariff {
	t.Periods = slices.Clone(t.Periods)
	t.Squads = slices.Clone(t.Squads)
	return t
}

func (s *Store) Tariffs(_ context.Context, onlyActive bool) ([]core.Tariff, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []core.Tariff
	for _, t := range s.st.tariffs {
		if !onlyActive || t.Active {
			out = append(out, cloneTariff(t))
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Sort != out[j].Sort {
			return out[i].Sort < out[j].Sort
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

func (s *Store) SubByUser(_ context.Context, userID int64) (core.Subscription, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return (&tx{st: s.st}).SubByUser(userID)
}

// Tasks отдаёт все задачи очереди, которые положило ядро, в порядке появления, и выполненные тоже.
func (s *Store) Tasks() []core.Task {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]core.Task, len(s.st.tasks))
	for i, r := range s.st.tasks {
		out[i] = r.Task
	}
	return out
}

// Pending считает невыполненные задачи вида kind.
func (s *Store) Pending(kind string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, r := range s.st.tasks {
		if r.Kind == kind && !r.done {
			n++
		}
	}
	return n
}

// SetUser подменяет абонента в обход ядра.
func (s *Store) SetUser(u core.User) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.st.users[u.ID] = u
}

// SetSub подменяет подписку в обход ядра: так тесты ставят нужное состояние.
func (s *Store) SetSub(sub core.Subscription) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.st.subs[sub.ID] = sub
}

func (t *tx) SetTrialUsed(userID int64) error {
	u, ok := t.st.users[userID]
	if !ok {
		return core.ErrNotFound
	}
	u.TrialUsed = true
	t.st.users[userID] = u
	return nil
}

func (t *tx) Tariff(id int64) (core.Tariff, error) {
	v, ok := t.st.tariffs[id]
	if !ok {
		return core.Tariff{}, core.ErrNotFound
	}
	return cloneTariff(v), nil
}

func (t *tx) SaveTariff(v core.Tariff) (int64, error) {
	if v.ID == 0 {
		v.ID = t.st.id()
	} else if old, ok := t.st.tariffs[v.ID]; ok {
		v.CreatedAt = old.CreatedAt
	} else {
		return 0, core.ErrNotFound
	}
	t.st.tariffs[v.ID] = cloneTariff(v)
	return v.ID, nil
}

func (t *tx) Sub(id int64) (core.Subscription, error) {
	s, ok := t.st.subs[id]
	if !ok {
		return s, core.ErrNotFound
	}
	return s, nil
}

func (t *tx) SubByUser(userID int64) (core.Subscription, error) {
	for _, s := range t.st.subs {
		if s.UserID == userID {
			return s, nil
		}
	}
	return core.Subscription{}, core.ErrNotFound
}

func (t *tx) SaveSub(s core.Subscription) (int64, error) {
	if s.ID == 0 {
		if _, err := t.SubByUser(s.UserID); err == nil {
			return 0, core.ErrDuplicate
		}
		s.ID = t.st.id()
	} else if _, ok := t.st.subs[s.ID]; !ok {
		return 0, core.ErrNotFound
	}
	t.st.subs[s.ID] = s
	return s.ID, nil
}

func (t *tx) ActiveSubsUntil(until time.Time) ([]core.Subscription, error) {
	var out []core.Subscription
	for _, s := range t.st.subs {
		if s.Status == core.SubActive && !s.ExpiresAt.After(until) {
			out = append(out, s)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (t *tx) Enqueue(kind string, payload []byte, at time.Time) (int64, error) {
	id := t.st.id()
	t.st.tasks = append(t.st.tasks, taskRow{
		Task:   core.Task{ID: id, Kind: kind, Payload: slices.Clone(payload), CreatedAt: at},
		nextAt: at,
	})
	return id, nil
}

func (t *tx) AddNotice(userID int64, kind, ref string, _ time.Time) (bool, error) {
	key := fmt.Sprintf("%d|%s|%s", userID, kind, ref)
	if t.st.notices[key] {
		return false, nil
	}
	t.st.notices[key] = true
	return true, nil
}
