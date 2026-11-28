package coretest

import (
	"context"
	"encoding/json"
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

func (s *Store) Payments(_ context.Context, f core.PaymentFilter, limit int) ([]core.PaymentRow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var all []core.Payment
	for _, p := range s.st.pays {
		all = append(all, p)
	}
	sort.Slice(all, func(i, j int) bool { return all[i].ID > all[j].ID })
	var out []core.PaymentRow
	for _, p := range all {
		switch {
		case len(out) >= limit, f.After > 0 && p.ID >= f.After, f.Status != "" && p.Status != f.Status,
			f.Gateway != "" && p.Gateway != f.Gateway, !f.From.IsZero() && p.CreatedAt.Before(f.From),
			!f.To.IsZero() && !p.CreatedAt.Before(f.To):
			continue
		}
		u := s.st.users[p.UserID]
		out = append(out, core.PaymentRow{Payment: p, Username: u.Username, TgID: u.TgID})
	}
	return out, nil
}

func (s *Store) PromoList(_ context.Context, after int64, limit int) ([]core.Promo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var all []core.Promo
	for _, p := range s.st.promos {
		if after == 0 || p.ID < after {
			all = append(all, p)
		}
	}
	sort.Slice(all, func(i, j int) bool { return all[i].ID > all[j].ID })
	return all[:min(len(all), limit)], nil
}

func (s *Store) PromoByID(_ context.Context, id int64) (core.Promo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.st.promos[id]
	if !ok {
		return p, core.ErrNotFound
	}
	return p, nil
}

func (s *Store) GatewayStats(_ context.Context) (map[string]core.GatewayStat, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[string]core.GatewayStat{}
	for _, p := range s.st.pays {
		st := out[p.Gateway]
		switch p.Status {
		case core.PayPaid:
			if p.PaidAt.After(st.LastPaidAt) {
				st.LastPaidAt = p.PaidAt
			}
		case core.PayPending:
			st.Pending++
		}
		out[p.Gateway] = st
	}
	return out, nil
}

func (s *Store) StatsData(_ context.Context, q core.StatsQuery) (core.StatsData, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	d := core.StatsData{Paid: map[int64]map[string]int64{}, Refunded: map[int64]map[string]int64{}}
	in := func(t time.Time) bool { return !t.Before(q.From) && t.Before(q.To) }
	day := func(t time.Time) int64 { return (t.Unix() + int64(q.OffsetSec)) / 86400 }
	add := func(m map[int64]map[string]int64, t time.Time, gw string, v int64) {
		if m[day(t)] == nil {
			m[day(t)] = map[string]int64{}
		}
		m[day(t)][gw] += v
	}
	firstPaid := map[int64]time.Time{}
	for _, p := range s.st.pays {
		if p.Status != core.PayPaid && p.Status != core.PayRefunded {
			continue
		}
		if in(p.PaidAt) {
			add(d.Paid, p.PaidAt, p.Gateway, p.Amount)
		}
		if first, ok := firstPaid[p.UserID]; !ok || p.PaidAt.Before(first) {
			firstPaid[p.UserID] = p.PaidAt
		}
		if at := cmpTime(p.RefundedAt, p.PaidAt); p.Status == core.PayRefunded && in(at) {
			add(d.Refunded, at, p.Gateway, p.Amount)
		}
	}
	for _, t := range firstPaid {
		if in(t) {
			d.FirstPaid++
		}
	}
	for _, u := range s.st.users {
		if in(u.CreatedAt) {
			d.NewUsers++
		}
	}
	users := map[int64]bool{}
	for _, sub := range s.st.subs {
		if u := s.st.users[sub.UserID]; u.TrialUsed && in(sub.CreatedAt) {
			d.Trials++
		}
		if sub.Status == core.SubActive && !sub.CreatedAt.After(q.At) && sub.ExpiresAt.After(q.At) {
			d.ActiveSubs++
		}
	}
	for _, r := range s.st.tasks {
		if r.done || r.Kind == core.KindNotifyAdmin || r.Kind == core.KindNotifyOwner || !r.CreatedAt.Before(q.StuckBefore) {
			continue
		}
		d.StuckTasks++
		if uid := s.taskUser(r.Task); uid != 0 && !users[uid] && len(d.StuckUsers) < 20 {
			users[uid] = true
			d.StuckUsers = append(d.StuckUsers, uid)
		}
	}
	sort.Slice(d.StuckUsers, func(i, j int) bool { return d.StuckUsers[i] < d.StuckUsers[j] })
	return d, nil
}

// cmpTime отдаёт время возврата, а если его нет (старая запись), время оплаты.
func cmpTime(refunded, paid time.Time) time.Time {
	if refunded.IsZero() {
		return paid
	}
	return refunded
}

// taskUser находит абонента задачи очереди: синхронизации по подписке или уведомления.
func (s *Store) taskUser(t core.Task) int64 {
	switch t.Kind {
	case core.KindPanelSync:
		var p core.PanelSyncTask
		if json.Unmarshal(t.Payload, &p) == nil {
			return s.st.subs[p.SubID].UserID
		}
	case core.KindNotifyUser:
		var n core.NotifyTask
		if json.Unmarshal(t.Payload, &n) == nil {
			return n.UserID
		}
	}
	return 0
}

func (t *tx) PromoByID(id int64) (core.Promo, error) {
	p, ok := t.st.promos[id]
	if !ok {
		return p, core.ErrNotFound
	}
	return p, nil
}

func (s *Store) AdminByTg(_ context.Context, tgID int64) (core.Admin, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return (&tx{st: s.st}).Admin(tgID)
}

func (s *Store) AdminSession(_ context.Context, tokenHash []byte) (core.Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return (&tx{st: s.st}).Session(tokenHash)
}

// Строки настроек и текстов в памяти: настоящие кэши держат настройки и тексты, здесь то, что записала транзакция.
func (t *tx) SaveSetting(key, value string, _ time.Time) error { t.st.kv["s:"+key] = value; return nil }
func (t *tx) DeleteSetting(key string) error                   { delete(t.st.kv, "s:"+key); return nil }
func (t *tx) SaveText(key, lang, value string, _ time.Time) error {
	t.st.kv["t:"+key+"|"+lang] = value
	return nil
}
func (t *tx) DeleteText(key, lang string) error { delete(t.st.kv, "t:"+key+"|"+lang); return nil }

// StoredSetting отдаёт строку настройки, записанную транзакцией ядра, для проверок в тестах.
func (s *Store) StoredSetting(key string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.st.kv["s:"+key]
	return v, ok
}

// StoredText отдаёт правку текста, записанную транзакцией ядра, для проверок в тестах.
func (s *Store) StoredText(key, lang string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.st.kv["t:"+key+"|"+lang]
	return v, ok
}
