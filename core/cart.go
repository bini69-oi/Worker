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
	switch {
	case err == nil, errors.Is(err, ErrNotEnoughMoney):
	default:
		c.log.Warn("умная корзина не купила", "user", userID, "err", err)
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
		s.AutoRenew, s.UpdatedAt = on, c.clock.Now()
		_, err = tx.SaveSub(s)
		return err
	})
}

// AutoRenewDue продлевает с баланса подписки с автопродлением за autorenew.days_before дней до окончания
// тем же сроком, что куплен в прошлый раз. Не хватает денег: абонент один раз получает сумму, которой не хватает.
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
		if !s.AutoRenew || s.Trial || !s.ExpiresAt.After(now) {
			continue
		}
		ok, err := c.renewOne(ctx, s, now)
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

// renewOne продлевает одну подписку. Одна попытка на дату окончания: после продления дата другая.
func (c *Core) renewOne(ctx context.Context, s Subscription, now time.Time) (bool, error) {
	renewed := false
	err := c.tx(ctx, func(tx Tx) error {
		fresh, err := tx.AddNotice(s.UserID, "autorenew", fmt.Sprintf("%d:%d", s.ID, s.ExpiresAt.Unix()), now)
		if err != nil || !fresh {
			return err
		}
		sub, err := c.buyTx(tx, s.UserID, s.TariffID, s.PeriodDays, now)
		var need *NeedMoney
		if errors.As(err, &need) {
			// Деньги не списаны: move отказал до записи. Сообщаем, сколько не хватает.
			return enqueueNotify(tx, NotifyTask{UserID: s.UserID, Key: "notify.autorenew_fail",
				Vars: map[string]string{"missing": strconv.FormatInt(need.Missing, 10)}}, now)
		}
		if err != nil {
			return err
		}
		renewed = true
		return enqueueNotify(tx, NotifyTask{UserID: s.UserID, Key: "notify.autorenew_ok",
			Vars: map[string]string{"until": sub.ExpiresAt.Format("02.01.2006")}}, now)
	})
	return renewed, err
}
