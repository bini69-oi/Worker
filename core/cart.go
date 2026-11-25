package core

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"
)

// PendingBuy это запомненный выбор абонента: что купить, когда придут деньги.
type PendingBuy struct {
	UserID    int64
	TariffID  int64
	Days      int
	CreatedAt time.Time
}

// cartTTL это срок, после которого запомненный выбор устаревает.
const cartTTL = 24 * time.Hour

// RememberBuy запоминает выбор абонента, которому не хватило денег. Новый выбор заменяет старый.
func (c *Core) RememberBuy(ctx context.Context, userID, tariffID int64, days int) error {
	return c.tx(ctx, func(tx Tx) error {
		t, err := tx.Tariff(tariffID)
		if err != nil {
			return err
		}
		if _, ok := t.period(days); !ok {
			return ErrNoPeriod
		}
		return tx.SetPendingBuy(PendingBuy{UserID: userID, TariffID: tariffID, Days: days, CreatedAt: c.clock.Now()})
	})
}

// buyFromCart покупает запомненный выбор после пополнения. Идёт отдельной транзакцией после зачисления,
// поэтому ошибка покупки никогда не отменяет само зачисление. Не хватает денег: выбор ждёт следующего пополнения.
func (c *Core) buyFromCart(ctx context.Context, userID int64) {
	var pb PendingBuy
	err := c.store.Tx(ctx, func(tx Tx) error {
		var err error
		pb, err = tx.PendingBuy(userID)
		return err
	})
	if err != nil {
		return
	}
	if c.clock.Now().Sub(pb.CreatedAt) > cartTTL {
		_ = c.tx(ctx, func(tx Tx) error { return tx.DeletePendingBuy(userID) })
		return
	}
	err = c.tx(ctx, func(tx Tx) error {
		now := c.clock.Now()
		sub, err := c.buyTx(tx, userID, pb.TariffID, pb.Days, now)
		if err != nil {
			return err
		}
		if err := tx.DeletePendingBuy(userID); err != nil {
			return err
		}
		return enqueueNotify(tx, NotifyTask{UserID: userID, Key: "notify.bought",
			Vars: map[string]string{"days": strconv.Itoa(pb.Days), "until": sub.ExpiresAt.Format("02.01.2006")}}, now)
	})
	if permanent(err) {
		// Этот выбор уже не купить. Остальные ошибки (нехватка денег, сбой базы) оставляют выбор до следующего раза.
		c.log.Warn("умная корзина: выбор больше не купить", "user", userID, "err", err)
		_ = c.tx(ctx, func(tx Tx) error { return tx.DeletePendingBuy(userID) })
	}
}

// SetAutoRenew включает или выключает автопродление подписки с баланса.
func (c *Core) SetAutoRenew(ctx context.Context, userID int64, on bool) error {
	return c.tx(ctx, func(tx Tx) error {
		s, err := tx.SubByUser(userID)
		if err != nil {
			return err
		}
		if s.Trial {
			return ErrInvalid
		}
		if on {
			// Продлевать можно только тем сроком, который у тарифа есть (подписка с промокода могла быть на 7 дней).
			t, err := tx.Tariff(s.TariffID)
			if err != nil {
				return err
			}
			if _, ok := t.period(s.PeriodDays); !ok {
				return ErrNoPeriod
			}
		}
		s.AutoRenew, s.UpdatedAt = on, c.clock.Now()
		_, err = tx.SaveSub(s)
		return err
	})
}

// permanent говорит, что ошибка покупки не пройдёт сама: тариф удалён или скрыт, срока нет, бан, другой тариф.
func permanent(err error) bool {
	for _, e := range []error{ErrNoPeriod, ErrNotFound, ErrTariffHidden, ErrOtherTariff} {
		if errors.Is(err, e) {
			return true
		}
	}
	return false
}

// AutoRenewDue продлевает с баланса подписки с автопродлением за autorenew.days_before дней до окончания
// тем же сроком, что куплен в прошлый раз. Окно не больше половины срока подписки, поэтому одна подписка
// не продлится дважды подряд. Не хватает денег: абонент один раз получает сумму, которой не хватает,
// а попытки идут дальше, и после пополнения продление пройдёт.
func (c *Core) AutoRenewDue(ctx context.Context) (int, error) {
	now := c.clock.Now()
	before := time.Duration(c.cfg.Int("autorenew.days_before")) * day
	var subs []Subscription
	if err := c.store.Tx(ctx, func(tx Tx) error {
		var err error
		subs, err = tx.ActiveSubsUntil(now.Add(before))
		return err
	}); err != nil {
		return 0, err
	}
	renewed := 0
	for _, s := range subs {
		window := min(before, time.Duration(s.PeriodDays)*day/2)
		if !s.AutoRenew || s.Trial || !s.ExpiresAt.After(now) || s.ExpiresAt.Sub(now) > window {
			continue
		}
		ok, err := c.renewOne(ctx, s, window, now)
		if err != nil {
			c.log.Warn("автопродление не прошло", "sub", s.ID, "err", err)
			continue
		}
		if ok {
			renewed++
		}
	}
	return renewed, nil
}

// renewOne продлевает одну подписку. После продления дата окончания уходит за окно, повтора нет.
// Окно проверяется заново внутри транзакции по свежей подписке: если абонент сам продлил её, пока шёл
// прогон, второго списания не будет.
func (c *Core) renewOne(ctx context.Context, s Subscription, window time.Duration, now time.Time) (bool, error) {
	var sub Subscription
	skipped := false
	err := c.tx(ctx, func(tx Tx) error {
		cur, err := tx.Sub(s.ID)
		if err != nil {
			return err
		}
		if !cur.AutoRenew || cur.Status != SubActive || cur.ExpiresAt.Sub(now) > window {
			skipped = true
			return nil
		}
		sub, err = c.buyTx(tx, s.UserID, s.TariffID, s.PeriodDays, now)
		if err != nil {
			return err
		}
		return enqueueNotify(tx, NotifyTask{UserID: s.UserID, Key: "notify.autorenew_ok",
			Vars: map[string]string{"until": sub.ExpiresAt.Format("02.01.2006")}}, now)
	})
	if err == nil {
		return !skipped, nil
	}
	if errors.Is(err, ErrBanned) {
		return false, nil // забаненного не продлеваем, но и автопродление не выключаем: бан могут снять
	}
	var need *NeedMoney
	switch {
	case errors.As(err, &need):
		// Сообщаем один раз на дату окончания, а пробовать будем и дальше.
		return false, c.tx(ctx, func(tx Tx) error {
			fresh, err := tx.AddNotice(s.UserID, "autorenew_fail", fmt.Sprintf("%d:%d", s.ID, s.ExpiresAt.Unix()), now)
			if err != nil || !fresh {
				return err
			}
			return enqueueNotify(tx, NotifyTask{UserID: s.UserID, Key: "notify.autorenew_fail",
				Vars: map[string]string{"missing": strconv.FormatInt(need.Missing, 10)}}, now)
		})
	case permanent(err):
		// Продлить этим сроком больше нельзя: выключаем автопродление и говорим абоненту.
		return false, c.tx(ctx, func(tx Tx) error {
			cur, err := tx.Sub(s.ID)
			if err != nil {
				return err
			}
			cur.AutoRenew, cur.UpdatedAt = false, now
			if _, err := tx.SaveSub(cur); err != nil {
				return err
			}
			return enqueueNotify(tx, NotifyTask{UserID: s.UserID, Key: "notify.autorenew_off"}, now)
		})
	}
	return false, err
}
