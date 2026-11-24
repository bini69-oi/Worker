package core

import (
	"context"
	"errors"
	"time"
)

// Buy списывает цену срока с баланса и выдаёт или продлевает подписку одной транзакцией.
// Панель получит изменения через очередь, поэтому её недоступность покупке не мешает.
//
// Тот же тариф продлевается от даты окончания, если подписка живая, иначе от сегодня.
// Пробный период заменяется купленным тарифом, оставшиеся дни сохраняются.
// Другой тариф при живой подписке не продаётся: смена тарифа с пересчётом будет позже.
func (c *Core) Buy(ctx context.Context, userID, tariffID int64, days int) (Subscription, error) {
	var out Subscription
	err := c.store.Tx(ctx, func(tx Tx) error {
		if _, err := activeUser(tx, userID); err != nil {
			return err
		}
		t, err := tx.Tariff(tariffID)
		if err != nil {
			return err
		}
		p, ok := t.period(days)
		if !ok {
			return ErrNoPeriod
		}
		now := c.clock.Now()
		sub, err := tx.SubByUser(userID)
		if err != nil && !errors.Is(err, ErrNotFound) {
			return err
		}
		exists := err == nil
		live := exists && sub.Status == SubActive && sub.ExpiresAt.After(now)
		same := exists && !sub.Trial && sub.TariffID == tariffID

		// Скрытый тариф продлевает только тот, у кого он уже есть.
		if !t.Active && !same {
			return ErrTariffHidden
		}
		if live && !sub.Trial && !same {
			return ErrOtherTariff
		}

		if p.Price > 0 {
			if _, err := move(tx, now, LedgerEntry{UserID: userID, Amount: -p.Price, Reason: ReasonBuy, RefType: "tariff", Comment: t.Name}); err != nil {
				return needMoney(tx, userID, p.Price, err)
			}
		}

		base := now
		if live {
			base = sub.ExpiresAt
		}
		if !exists {
			sub = Subscription{UserID: userID, CreatedAt: now}
		}
		sub.TariffID, sub.PeriodDays, sub.Price, sub.Trial = tariffID, days, p.Price, false
		sub.ExpiresAt = base.Add(time.Duration(days) * day)
		sub.Status, sub.UpdatedAt = SubActive, now
		if sub.ID, err = tx.SaveSub(sub); err != nil {
			return err
		}
		out = sub
		return enqueueSync(tx, sub.ID, now)
	})
	return out, err
}

// needMoney превращает «не хватает денег» в ошибку с недостающей суммой.
func needMoney(tx Tx, userID, price int64, err error) error {
	if !errors.Is(err, ErrNotEnoughMoney) {
		return err
	}
	u, uerr := tx.User(userID)
	if uerr != nil {
		return err
	}
	return &NeedMoney{Missing: price - u.Balance}
}
