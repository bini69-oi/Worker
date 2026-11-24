package coretest

import (
	"context"
	"strings"
	"time"

	"github.com/bini69-oi/worker/core"
)

type limitRow struct {
	count int
	until time.Time
}

func (t *tx) UserByRef(code string) (core.User, error) {
	for _, u := range t.st.users {
		if u.RefCode == code && code != "" {
			return u, nil
		}
	}
	return core.User{}, core.ErrNotFound
}

func (t *tx) Promo(code string) (core.Promo, error) {
	for _, p := range t.st.promos {
		if strings.EqualFold(p.Code, code) {
			return p, nil
		}
	}
	return core.Promo{}, core.ErrNotFound
}

func (t *tx) SavePromo(p core.Promo) (int64, error) {
	for _, old := range t.st.promos {
		if old.ID != p.ID && strings.EqualFold(old.Code, p.Code) {
			return 0, core.ErrDuplicate
		}
	}
	if p.ID == 0 {
		p.ID = t.st.id()
	} else if old, ok := t.st.promos[p.ID]; ok {
		p.Uses, p.CreatedAt = old.Uses, old.CreatedAt
	} else {
		return 0, core.ErrNotFound
	}
	t.st.promos[p.ID] = p
	return p.ID, nil
}

func (t *tx) UsePromo(promoID, userID int64, _ time.Time) error {
	key := [2]int64{promoID, userID}
	if t.st.promoUses[key] {
		return core.ErrDuplicate
	}
	p, ok := t.st.promos[promoID]
	if !ok {
		return core.ErrNotFound
	}
	if p.MaxUses > 0 && p.Uses >= p.MaxUses {
		return core.ErrPromoExhausted
	}
	p.Uses++
	t.st.promos[promoID] = p
	t.st.promoUses[key] = true
	return nil
}

func (t *tx) Limit(key string) (int, time.Time, error) {
	r := t.st.limits[key]
	return r.count, r.until, nil
}

func (t *tx) SetLimit(key string, count int, until time.Time) error {
	t.st.limits[key] = limitRow{count, until}
	return nil
}

func (s *Store) RefStats(_ context.Context, userID int64) (core.RefStats, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var st core.RefStats
	buyers := map[int64]bool{}
	for _, u := range s.st.users {
		if u.ReferrerID == userID {
			st.Invited++
			buyers[u.ID] = false
		}
	}
	for _, e := range s.st.ledger {
		if _, ok := buyers[e.UserID]; ok && e.Reason == core.ReasonBuy {
			buyers[e.UserID] = true
		}
		if e.UserID == userID && e.Reason == core.ReasonReferral && e.RefType == "ref_buy" {
			st.Earned += e.Amount
		}
	}
	for _, bought := range buyers {
		if bought {
			st.Bought++
		}
	}
	return st, nil
}
