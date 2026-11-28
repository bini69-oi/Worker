package coretest

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/bini69-oi/worker/core"
)

func (t *tx) CreateBroadcast(b core.Broadcast) (int64, error) {
	if t.st.bcs == nil {
		t.st.bcs = map[int64]core.Broadcast{}
	}
	b.ID = t.st.id()
	t.st.bcs[b.ID] = b
	return b.ID, nil
}

func (t *tx) Broadcast(id int64) (core.Broadcast, error) {
	b, ok := t.st.bcs[id]
	if !ok {
		return b, core.ErrNotFound
	}
	return b, nil
}

func (t *tx) RunningBroadcast() (core.Broadcast, error) {
	for _, b := range t.st.bcs {
		if b.Status == core.BcRunning {
			return b, nil
		}
	}
	return core.Broadcast{}, core.ErrNotFound
}

func (t *tx) StartBroadcast(id int64, total int, at time.Time) (bool, error) {
	b, ok := t.st.bcs[id]
	if !ok || b.Status != core.BcDraft {
		return false, nil
	}
	if _, err := t.RunningBroadcast(); err == nil {
		return false, core.ErrDuplicate
	}
	b.Status, b.Total, b.StartedAt = core.BcRunning, total, at
	t.st.bcs[id] = b
	return true, nil
}

func (t *tx) CancelBroadcast(id int64, at time.Time) (bool, error) {
	b, ok := t.st.bcs[id]
	if !ok || (b.Status != core.BcDraft && b.Status != core.BcRunning) {
		return false, nil
	}
	b.Status, b.DoneAt = core.BcCancelled, at
	t.st.bcs[id] = b
	return true, nil
}

func (t *tx) BroadcastStep(id, userID int64, sent, failed, blocked int) (bool, error) {
	b, ok := t.st.bcs[id]
	if !ok || (b.Status != core.BcRunning && b.Status != core.BcCancelled) {
		return false, nil
	}
	b.CursorUserID = userID
	b.Sent, b.Failed, b.Blocked = b.Sent+sent, b.Failed+failed, b.Blocked+blocked
	t.st.bcs[id] = b
	return b.Status == core.BcRunning, nil
}

func (t *tx) FinishBroadcast(id int64, at time.Time) (bool, error) {
	b, ok := t.st.bcs[id]
	if !ok || b.Status != core.BcRunning {
		return false, nil
	}
	b.Status, b.DoneAt = core.BcDone, at
	t.st.bcs[id] = b
	return true, nil
}

func (st *state) segment(seg string, now time.Time) []core.User {
	var out []core.User
	for _, r := range st.userRows() {
		if r.User.BannedAt.IsZero() && r.User.UnreachableAt.IsZero() && r.Matches(seg, now) {
			out = append(out, r.User)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func (t *tx) SegmentCount(seg string, now time.Time) (int, error) {
	return len(t.st.segment(seg, now)), nil
}

func (s *Store) SegmentCount(_ context.Context, seg string, now time.Time) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.st.segment(seg, now)), nil
}

func (s *Store) SegmentUsers(_ context.Context, seg string, after int64, now time.Time, limit int) ([]core.User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []core.User
	for _, u := range s.st.segment(seg, now) {
		if u.ID > after && len(out) < limit {
			out = append(out, u)
		}
	}
	return out, nil
}

func (s *Store) Broadcasts(_ context.Context, after int64, limit int) ([]core.Broadcast, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var all []core.Broadcast
	for _, b := range s.st.bcs {
		if after == 0 || b.ID < after {
			all = append(all, b)
		}
	}
	sort.Slice(all, func(i, j int) bool { return all[i].ID > all[j].ID })
	return all[:min(len(all), limit)], nil
}

func (s *Store) BroadcastByID(_ context.Context, id int64) (core.Broadcast, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return (&tx{st: s.st}).Broadcast(id)
}

// SentMessage это одно сообщение рассылки, которое принял фейковый Telegram.
type SentMessage struct {
	TgID       int64
	Text       string
	ButtonText string
	ButtonURL  string
}

// Sender это фейковый Telegram для рассылок: помнит, что и кому ушло, и умеет отвечать отказами.
type Sender struct {
	mu   sync.Mutex
	Sent []SentMessage
	// Gone это Telegram ID, которые заблокировали бота.
	Gone map[int64]bool
	// Fail это Telegram ID, на которых отправка падает прочей ошибкой.
	Fail map[int64]error
	// Wait это сколько секунд просить подождать (ответ 429) на ближайшей отправке. Сбрасывается после неё.
	Wait int
	// RejectMarkup заставляет Telegram отвергать разметку любого сообщения.
	RejectMarkup bool
	// OnSend зовётся после каждой удачной отправки: тест успевает отменить рассылку «посередине».
	OnSend func(n int)
}

func (s *Sender) Send(_ context.Context, tgID int64, text, btnText, btnURL string) error {
	s.mu.Lock()
	if s.Wait > 0 {
		w := s.Wait
		s.Wait = 0
		s.mu.Unlock()
		return &core.RetryAfter{Seconds: w}
	}
	if s.RejectMarkup {
		s.mu.Unlock()
		return core.ErrMarkupRejected
	}
	if s.Gone[tgID] {
		s.mu.Unlock()
		return core.ErrRecipientGone
	}
	if err := s.Fail[tgID]; err != nil {
		s.mu.Unlock()
		return err
	}
	s.Sent = append(s.Sent, SentMessage{tgID, text, btnText, btnURL})
	n, hook := len(s.Sent), s.OnSend
	s.mu.Unlock()
	if hook != nil {
		hook(n)
	}
	return nil
}

// Messages отдаёт копию отправленного.
func (s *Sender) Messages() []SentMessage {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]SentMessage(nil), s.Sent...)
}
