package core

import (
	"context"
	"errors"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Защита от перебора промокодов: не больше promoTries неверных кодов за promoWindow с одного абонента
// и не больше promoAllTries со всех вместе, чтобы перебор с пачки аккаунтов тоже упёрся в предел.
const (
	promoTries    = 10
	promoAllTries = 300
	promoWindow   = 10 * time.Minute
	promoAllKey   = "promo:all"
)

// promoCodeRe это вид кода. Не короче 6 знаков: короткий код подбирается перебором.
var promoCodeRe = regexp.MustCompile(`^[A-Z0-9_-]{6,32}$`)

// normCode приводит код к виду из базы: без пробелов по краям, заглавными.
func normCode(code string) string { return strings.ToUpper(strings.TrimSpace(code)) }

// SavePromo создаёт или меняет промокод от имени админа с правом «промокоды».
func (c *Core) SavePromo(ctx context.Context, actorTg int64, p Promo) (Promo, error) {
	p.Code = normCode(p.Code)
	if !promoCodeRe.MatchString(p.Code) || p.Value <= 0 || p.MaxUses < 0 ||
		(p.Kind != PromoMoney && p.Kind != PromoDays) || (p.Kind == PromoMoney && p.TariffID != 0) {
		return p, ErrInvalid
	}
	if p.ID == 0 {
		p.CreatedAt = c.clock.Now()
	}
	err := c.asAdmin(ctx, actorTg, PermPromo, "promo.save", func(tx Tx, a Admin, now time.Time) (string, map[string]string, error) {
		return c.savePromoTx(tx, a, &p, now)
	})
	if errors.Is(err, errNeedGrant) {
		c.deny(ctx, actorTg, "promo.save.money")
		return p, ErrForbidden
	}
	return p, err
}

// maxPromoMoney это самая большая сумма денежного промокода: 10 000 ₽ в копейках.
const maxPromoMoney = 1_000_000

// errNeedGrant значит, что денежный промокод хочет сохранить админ без права grant. Наружу не выходит.
var errNeedGrant = errors.New("денежный промокод требует права grant")

// savePromoTx сохраняет промокод внутри транзакции действия админа. Денежный промокод это деньги абонентам,
// поэтому его заводит и включает только админ с правом grant, а помощник так начисляющий деньги шлёт владельцу тревогу.
func (c *Core) savePromoTx(tx Tx, a Admin, p *Promo, now time.Time) (string, map[string]string, error) {
	if p.Kind == PromoMoney {
		if !a.Owner && !slices.Contains(a.Perms, PermGrant) {
			return "", nil, errNeedGrant
		}
		if err := c.helperMoney(tx, a, "promo.save", 0, p.Value, now); err != nil {
			return "", nil, err
		}
	}
	if p.Kind == PromoMoney && p.Value > maxPromoMoney || p.Kind == PromoDays && p.Value > maxGrantDays {
		return "", nil, ErrInvalid
	}
	if p.TariffID != 0 {
		if _, err := tx.Tariff(p.TariffID); err != nil {
			return "", nil, err
		}
	}
	var err error
	if p.ID, err = tx.SavePromo(*p); err != nil {
		return "", nil, err
	}
	return p.Code, map[string]string{"kind": string(p.Kind), "value": itoa(p.Value), "max_uses": itoa(int64(p.MaxUses)),
		"active": strconv.FormatBool(p.Active)}, nil
}

// ApplyPromo активирует промокод. Регистр и пробелы по краям не важны. Каждая попытка засчитывается заранее,
// своей транзакцией, а удачная возвращает попытку назад. После 10 неудачных за 10 минут любые попытки
// отклоняются до конца окна, даже верные. Так пачка параллельных запросов счётчик не обходит.
func (c *Core) ApplyPromo(ctx context.Context, userID int64, code string) (Promo, error) {
	if err := c.serving(ctx, userID); err != nil {
		return Promo{}, err
	}
	key := "promo:" + itoa(userID)
	now := c.clock.Now()
	alarm := false
	if err := c.tx(ctx, func(tx Tx) error {
		if err := countTry(tx, key, promoTries, promoWindow, now); err != nil {
			return err
		}
		err := countTry(tx, promoAllKey, promoAllTries, promoWindow, now)
		alarm = errors.Is(err, ErrTooManyTries)
		return err
	}); err != nil {
		if alarm {
			c.promoAlarm(ctx, now)
		}
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
		if err := untry(tx, key); err != nil {
			return err
		}
		return untry(tx, promoAllKey)
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

// countTry засчитывает попытку по ключу заранее. Больше max попыток за окно: ErrTooManyTries до конца окна.
func countTry(tx Tx, key string, max int, window time.Duration, now time.Time) error {
	count, until, err := tx.Limit(key)
	if err != nil {
		return err
	}
	if now.Before(until) && count >= max {
		return ErrTooManyTries
	}
	if !now.Before(until) {
		count, until = 0, now.Add(window)
	}
	return tx.SetLimit(key, count+1, until)
}

// untry возвращает удачную попытку назад.
func untry(tx Tx, key string) error {
	count, until, err := tx.Limit(key)
	if err != nil || count == 0 {
		return err
	}
	return tx.SetLimit(key, count-1, until)
}

// promoAlarm сообщает владельцу, что промокоды перебирают с многих аккаунтов. Один раз за окно.
func (c *Core) promoAlarm(ctx context.Context, now time.Time) {
	_ = c.tx(ctx, func(tx Tx) error {
		first, err := tx.AddNotice(0, "promo_bruteforce", now.Truncate(promoWindow).Format(time.RFC3339), now)
		if err != nil || !first {
			return err
		}
		return c.alert(tx, "admin.promo_bruteforce", nil, now)
	})
}
