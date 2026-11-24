package core

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"
)

// Защита от перебора промокодов: не больше promoTries неверных кодов за promoWindow.
const (
	promoTries  = 10
	promoWindow = 10 * time.Minute
)

var promoCodeRe = regexp.MustCompile(`^[A-Z0-9_-]{3,32}$`)

// normCode приводит код к виду из базы: без пробелов по краям, заглавными.
func normCode(code string) string { return strings.ToUpper(strings.TrimSpace(code)) }

// SavePromo создаёт или меняет промокод. Это действие админа.
func (c *Core) SavePromo(ctx context.Context, p Promo) (Promo, error) {
	p.Code = normCode(p.Code)
	if !promoCodeRe.MatchString(p.Code) || p.Value <= 0 || p.MaxUses < 0 ||
		(p.Kind != PromoMoney && p.Kind != PromoDays) || (p.Kind == PromoMoney && p.TariffID != 0) {
		return p, ErrInvalid
	}
	if p.ID == 0 {
		p.CreatedAt = c.clock.Now()
	}
	err := c.tx(ctx, func(tx Tx) error {
		if p.TariffID != 0 {
			if _, err := tx.Tariff(p.TariffID); err != nil {
				return err
			}
		}
		var err error
		p.ID, err = tx.SavePromo(p)
		return err
	})
	return p, err
}

// ApplyPromo активирует промокод. Регистр и пробелы по краям не важны. Каждая попытка засчитывается заранее,
// своей транзакцией, а удачная возвращает попытку назад. После 10 неудачных за 10 минут любые попытки
// отклоняются до конца окна, даже верные. Так пачка параллельных запросов счётчик не обходит.
func (c *Core) ApplyPromo(ctx context.Context, userID int64, code string) (Promo, error) {
	key := "promo:" + itoa(userID)
	now := c.clock.Now()
	if err := c.tx(ctx, func(tx Tx) error {
		count, until, err := tx.Limit(key)
		if err != nil {
			return err
		}
		if now.Before(until) && count >= promoTries {
			return ErrTooManyTries
		}
		if !now.Before(until) {
			count, until = 0, now.Add(promoWindow)
		}
		return tx.SetLimit(key, count+1, until)
	}); err != nil {
		return Promo{}, err
	}

	var out Promo
	err := c.tx(ctx, func(tx Tx) error {
		u, err := activeUser(tx, userID)
		if err != nil {
			return err
		}
		p, err := tx.Promo(normCode(code))
		if err != nil {
			return err
		}
		if err := c.promoAllowed(tx, p, u, now); err != nil {
			return err
		}
		if err := tx.UsePromo(p.ID, userID, now); err != nil {
			if errors.Is(err, ErrDuplicate) {
				return ErrPromoUsed
			}
			return err
		}
		if err := c.grantPromo(tx, p, u, now); err != nil {
			return err
		}
		out = p
		// Удачная попытка не в счёт.
		count, until, err := tx.Limit(key)
		if err != nil || count == 0 {
			return err
		}
		return tx.SetLimit(key, count-1, until)
	})
	if errors.Is(err, ErrNotFound) {
		return out, ErrPromoNotFound
	}
	return out, err
}

func (c *Core) promoAllowed(tx Tx, p Promo, u User, now time.Time) error {
	if !p.Active {
		return ErrPromoNotFound
	}
	if !p.ExpiresAt.IsZero() && !now.Before(p.ExpiresAt) {
		return ErrPromoExpired
	}
	if p.MaxUses > 0 && p.Uses >= p.MaxUses {
		return ErrPromoExhausted
	}
	if p.NewOnly {
		// Новый абонент это тот, кто ещё ничего не покупал: пробный период не в счёт.
		sub, err := tx.SubByUser(u.ID)
		if err == nil && !sub.Trial {
			return ErrPromoNewOnly
		}
		if err != nil && !errors.Is(err, ErrNotFound) {
			return err
		}
	}
	return nil
}

// grantPromo выдаёт то, что даёт код: деньги на баланс или дни к подписке.
func (c *Core) grantPromo(tx Tx, p Promo, u User, now time.Time) error {
	if p.Kind == PromoMoney {
		_, err := move(tx, now, LedgerEntry{UserID: u.ID, Amount: p.Value, Reason: ReasonPromo, RefType: "promo", Comment: p.Code})
		return err
	}
	sub, err := tx.SubByUser(u.ID)
	switch {
	case errors.Is(err, ErrNotFound):
		if p.TariffID == 0 {
			return ErrPromoNeedSub
		}
		sub = Subscription{UserID: u.ID, TariffID: p.TariffID, PeriodDays: int(p.Value), CreatedAt: now, ExpiresAt: now}
	case err != nil:
		return err
	}
	base := now
	if sub.Status == SubActive && sub.ExpiresAt.After(now) {
		base = sub.ExpiresAt
	}
	sub.ExpiresAt = base.Add(time.Duration(p.Value) * day)
	sub.Status, sub.UpdatedAt = SubActive, now
	if sub.ID, err = tx.SaveSub(sub); err != nil {
		return err
	}
	return enqueueSync(tx, sub.ID, now)
}
