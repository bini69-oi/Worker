package core

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"
)

// maxExtraDevices это предел докупленных устройств на подписку.
const maxExtraDevices = 10

// devicesCost это цена n докупленных устройств за days дней: device_month за 30 дней, с округлением вверх.
func devicesCost(c *Core, n, days int) int64 {
	month := c.cfg.Int("addons.device_month")
	if n <= 0 || days <= 0 || month <= 0 {
		return 0
	}
	return int64(n) * ((month*int64(days) + 29) / 30)
}

// CancelPlan отменяет ждущую смену тарифа и возвращает уплаченное на баланс.
func (c *Core) CancelPlan(ctx context.Context, userID int64) error {
	return c.tx(ctx, func(tx Tx) error {
		sub, err := tx.SubByUser(userID)
		if err != nil {
			return err
		}
		if sub.NextTariffID == 0 {
			return ErrNotFound
		}
		return c.dropPlan(tx, sub, c.clock.Now())
	})
}

// dropPlan стирает ждущую смену и возвращает её цену на баланс одной транзакцией.
func (c *Core) dropPlan(tx Tx, sub Subscription, now time.Time) error {
	if sub.NextPrice > 0 {
		if _, err := move(tx, now, LedgerEntry{UserID: sub.UserID, Amount: sub.NextPrice, Reason: ReasonRefund,
			RefType: "plan_cancel", Comment: "отмена смены тарифа"}); err != nil {
			return err
		}
	}
	sub.NextTariffID, sub.NextDays, sub.NextPrice, sub.NextBuyID, sub.UpdatedAt = 0, 0, 0, 0, now
	_, err := tx.SaveSub(sub)
	return err
}

// startPlan включает ждущую смену в день окончания: новый тариф продлевает срок от старой даты окончания.
// Тарифа больше нет: смена отменяется, деньги возвращаются, подписка истекает как обычно (switched=false).
func (c *Core) startPlan(tx Tx, s Subscription, now time.Time) (switched bool, err error) {
	t, err := tx.Tariff(s.NextTariffID)
	if errors.Is(err, ErrNotFound) {
		if err := c.dropPlan(tx, s, now); err != nil {
			return false, err
		}
		return false, enqueueNotify(tx, NotifyTask{UserID: s.UserID, Key: "notify.plan_dropped",
			Vars: map[string]string{"amount": itoa(s.NextPrice)}}, now)
	}
	if err != nil {
		return false, err
	}
	buyID := s.NextBuyID
	s.TariffID, s.PeriodDays, s.Price, s.Imported = t.ID, s.NextDays, s.NextPrice, false
	s.ExpiresAt = s.ExpiresAt.Add(time.Duration(s.NextDays) * day)
	s.NextTariffID, s.NextDays, s.NextPrice, s.NextBuyID = 0, 0, 0, 0
	s.ExtraTrafficBytes, s.ExtraTrafficUntil, s.ExtraDevices = 0, time.Time{}, 0 // у нового тарифа свои лимиты
	s.Status, s.UpdatedAt = SubActive, now
	if _, err := tx.SaveSub(s); err != nil {
		return false, err
	}
	if err := enqueueSync(tx, s.ID, now); err != nil {
		return false, err
	}
	buyer, err := tx.User(s.UserID)
	if err != nil {
		return false, err
	}
	// Бонус за смену платится сейчас, когда отменить её уже нельзя. Бесплатная смена (buyID 0) бонуса не даёт.
	if buyID != 0 {
		if err := c.referralBonus(tx, buyer, "ref_buy", buyID, s.Price, now); err != nil {
			return false, err
		}
	}
	return true, enqueueNotify(tx, NotifyTask{UserID: s.UserID, Key: "notify.plan_started",
		Vars: map[string]string{"tariff": t.Name, "until": s.ExpiresAt.Format("02.01.2006")}}, now)
}

// trafficPacks разбирает настройку addons.traffic вида «10:15000,50:50000»: гигабайты и цена в копейках.
func trafficPacks(setting string) map[int64]int64 {
	out := map[int64]int64{}
	for _, item := range strings.Split(setting, ",") {
		gb, price, ok := strings.Cut(strings.TrimSpace(item), ":")
		g, err1 := strconv.ParseInt(gb, 10, 64)
		p, err2 := strconv.ParseInt(price, 10, 64)
		if ok && err1 == nil && err2 == nil && g > 0 && g <= 10000 && p > 0 {
			out[g] = p
		}
	}
	return out
}
