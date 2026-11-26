package coretest

import (
	"context"
	"slices"
	"sort"
	"time"

	"github.com/bini69-oi/worker/core"
)

func (s *Store) SubsInPanel(_ context.Context) ([]core.Subscription, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []core.Subscription
	for _, sub := range s.st.subs {
		if sub.PanelUUID != "" {
			out = append(out, sub)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (s *Store) DueTasks(_ context.Context, now time.Time, limit int) ([]core.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []core.Task
	for _, r := range s.st.tasks {
		if !r.done && !r.nextAt.After(now) && len(out) < limit {
			t := r.Task
			t.Payload = slices.Clone(t.Payload)
			out = append(out, t)
		}
	}
	return out, nil
}

func (s *Store) row(id int64) *taskRow {
	for i := range s.st.tasks {
		if s.st.tasks[i].ID == id {
			return &s.st.tasks[i]
		}
	}
	return nil
}

func (s *Store) TaskDone(_ context.Context, id int64, _ time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r := s.row(id); r != nil {
		r.done = true
		return nil
	}
	return core.ErrNotFound
}

func (s *Store) TaskFailed(_ context.Context, id int64, attempts int, next time.Time, lastErr string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.row(id)
	if r == nil {
		return core.ErrNotFound
	}
	r.Attempts, r.nextAt, r.lastErr = attempts, next, lastErr
	return nil
}

func (s *Store) StuckTasks(_ context.Context, before time.Time) ([]core.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []core.Task
	for _, r := range s.st.tasks {
		if !r.done && !r.alerted && r.Kind != core.KindNotifyAdmin && r.Kind != core.KindNotifyOwner && r.CreatedAt.Before(before) {
			out = append(out, r.Task)
		}
	}
	return out, nil
}

func (t *tx) MarkAlerted(ids []int64) error {
	for i := range t.st.tasks {
		if slices.Contains(ids, t.st.tasks[i].ID) {
			t.st.tasks[i].alerted = true
		}
	}
	return nil
}
