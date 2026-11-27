package coretest

import (
	"context"
	"sort"
	"time"

	"github.com/bini69-oi/worker/core"
)

func (s *Store) PendingPayments(_ context.Context, before time.Time) ([]core.Payment, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []core.Payment
	for _, p := range s.st.pays {
		if p.Status == core.PayPending && p.CreatedAt.Before(before) {
			out = append(out, p)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// Payment отдаёт платёж в обход ядра, для проверок в тестах.
func (s *Store) Payment(id int64) core.Payment {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.st.pays[id]
}

func (t *tx) CreatePayment(p core.Payment) (int64, error) {
	p.ID = t.st.id()
	t.st.pays[p.ID] = p
	return p.ID, nil
}

func (t *tx) Payment(id int64) (core.Payment, error) {
	p, ok := t.st.pays[id]
	if !ok {
		return p, core.ErrNotFound
	}
	return p, nil
}

func (t *tx) Paid(userID int64) (bool, error) {
	for _, p := range t.st.pays {
		if p.UserID == userID && p.Status == core.PayPaid {
			return true, nil
		}
	}
	return false, nil
}

func (t *tx) PaymentByExt(gateway, externalID string) (core.Payment, error) {
	for _, p := range t.st.pays {
		if p.Gateway == gateway && p.ExternalID == externalID && externalID != "" {
			return p, nil
		}
	}
	return core.Payment{}, core.ErrNotFound
}

// extTaken повторяет уникальный ключ базы (касса, номер в кассе).
func (t *tx) extTaken(id int64, gateway, ext string) bool {
	for _, p := range t.st.pays {
		if p.ID != id && ext != "" && p.Gateway == gateway && p.ExternalID == ext {
			return true
		}
	}
	return false
}

func (t *tx) SetPaymentExt(id int64, ext string, extAmount int64, extCurrency string) error {
	p, ok := t.st.pays[id]
	if !ok {
		return core.ErrNotFound
	}
	if t.extTaken(id, p.Gateway, ext) {
		return core.ErrDuplicate
	}
	p.ExternalID, p.ExtAmount, p.ExtCurrency = ext, extAmount, extCurrency
	t.st.pays[id] = p
	return nil
}

func (t *tx) MarkPaid(id int64, ext string, at time.Time) (bool, error) {
	p, ok := t.st.pays[id]
	if !ok || p.Status == core.PayPaid || p.Status == core.PayRefunded {
		return false, nil
	}
	if ext != "" {
		if t.extTaken(id, p.Gateway, ext) {
			return false, core.ErrDuplicate
		}
		p.ExternalID = ext
	}
	p.Status, p.PaidAt = core.PayPaid, at
	t.st.pays[id] = p
	return true, nil
}

func (t *tx) SetPaymentStatus(id int64, from, to core.PayStatus) (bool, error) {
	p, ok := t.st.pays[id]
	if !ok || p.Status != from {
		return false, nil
	}
	p.Status = to
	t.st.pays[id] = p
	return true, nil
}

func (t *tx) MarkRefunded(id int64, at time.Time) (bool, error) {
	p, ok := t.st.pays[id]
	if !ok || p.Status != core.PayPaid {
		return false, nil
	}
	p.Status, p.RefundedAt = core.PayRefunded, at
	t.st.pays[id] = p
	return true, nil
}
