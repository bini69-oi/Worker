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
	if err := c.serving(ctx, userID); err != nil {
		return Subscription{}, err
	}
	var out Subscription
	err := c.tx(ctx, func(tx Tx) error {
		var err error
		if out, err = c.buyTx(tx, userID, tariffID, days, c.clock.Now()); err != nil {
			return err
		}
		// Купил сам: запомненная корзина больше не нужна, иначе следующее пополнение купило бы ещё раз.
		return tx.DeletePendingBuy(userID)
	})
	return out, err
}

// buyPlan это проверенная покупка: кто, что, за сколько и что у него уже есть.
type buyPlan struct {
	buyer  User
	tariff Tariff
	period Period
	sub    Subscription
	exists bool  // у абонента есть подписка
	live   bool  // и она ещё идёт
	queue  bool  // другой тариф при живой платной подписке: включится со следующего периода
	cost   int64 // к списанию: цена срока и докупленные устройства за этот срок
}

// planBuy проверяет покупку без списания. Её зовут покупка и расчёт цены, поэтому правила одни.
func planBuy(c *Core, tx Tx, userID, tariffID int64, days int, now time.Time) (buyPlan, error) {
	var b buyPlan
	var err error
	if b.buyer, err = activeUser(tx, userID); err != nil {
		return b, err
	}
	if b.tariff, err = tx.Tariff(tariffID); err != nil {
		return b, err
	}
	var ok bool
	if b.period, ok = b.tariff.period(days); !ok {
		return b, ErrNoPeriod
	}
	b.sub, err = tx.SubByUser(userID)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return b, err
	}
	b.exists = err == nil
	b.live = b.exists && b.sub.Status == SubActive && b.sub.ExpiresAt.After(now)
	same := b.exists && !b.sub.Trial && b.sub.TariffID == tariffID

	// Скрытый тариф продлевает только тот, у кого он уже есть.
	if !b.tariff.Active && !same {
		return b, ErrTariffHidden
	}
	// Ждущая смена одна: пока она не включилась или не отменена, ничего больше не покупается.
	if b.exists && b.sub.NextTariffID != 0 {
		return b, ErrPlanQueued
	}
	b.queue = b.live && !b.sub.Trial && !same
	b.cost = b.period.Price
	if same {
		b.cost += devicesCost(c, b.sub.ExtraDevices, days)
	}
	return b, nil
}

// buyTx это покупка внутри уже открытой транзакции. Её зовут Buy и списание автоподписки Stars.
func (c *Core) buyTx(tx Tx, userID, tariffID int64, days int, now time.Time) (Subscription, error) {
	b, err := planBuy(c, tx, userID, tariffID, days, now)
	if err != nil {
		return Subscription{}, err
	}
	buyer, t, sub, exists, live := b.buyer, b.tariff, b.sub, b.exists, b.live

	if b.cost > 0 {
		buy, err := move(tx, now, LedgerEntry{UserID: userID, Amount: -b.cost, Reason: ReasonBuy, RefType: "tariff", Comment: t.Name})
		if err != nil {
			return Subscription{}, needMoney(tx, userID, b.cost, err)
		}
		if err := c.referralBonus(tx, buyer, buy, b.cost, now); err != nil {
			return Subscription{}, err
		}
	}
	if b.queue {
		// Текущий тариф доживает срок, новый ждёт. Панель не меняется до дня смены.
		sub.NextTariffID, sub.NextDays, sub.NextPrice, sub.UpdatedAt = tariffID, days, b.cost, now
		_, err := tx.SaveSub(sub)
		return sub, err
	}

	base := now
	if live {
		base = sub.ExpiresAt
	}
	if !exists {
		sub = Subscription{UserID: userID, CreatedAt: now}
	}
	sub.TariffID, sub.PeriodDays, sub.Price, sub.Trial, sub.Imported = tariffID, days, b.period.Price, false, false
	sub.ExpiresAt = base.Add(time.Duration(days) * day)
	sub.Status, sub.UpdatedAt = SubActive, now
	if sub.ID, err = tx.SaveSub(sub); err != nil {
		return Subscription{}, err
	}
	return sub, enqueueSync(tx, sub.ID, now)
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
