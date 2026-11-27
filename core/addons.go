package core

import (
	"context"
	"slices"
	"time"
)

// BuyTraffic докупает пакет трафика из настройки addons.traffic к живой платной подписке.
// Пакет добавляется к лимиту тарифа и живёт, пока идёт подписка: при смене тарифа и окончании он сгорает.
func (c *Core) BuyTraffic(ctx context.Context, userID, gb int64) (Subscription, error) {
	if err := c.serving(ctx, userID); err != nil {
		return Subscription{}, err
	}
	price, ok := trafficPacks(c.cfg.String("addons.traffic"))[gb]
	if !ok {
		return Subscription{}, ErrAddonOff
	}
	return c.addon(ctx, userID, "traffic", func(sub *Subscription, t Tariff, now time.Time) (int64, error) {
		if t.TrafficBytes <= 0 {
			return 0, ErrUnlimited
		}
		sub.ExtraTrafficBytes += gb * gib
		return price, nil
	})
}

// BuyDevice докупает одно устройство до конца текущего периода: цена за 30 дней пропорционально оставшимся дням.
// Устройство переходит в следующий период, продление стоит с ним дороже.
func (c *Core) BuyDevice(ctx context.Context, userID int64) (Subscription, error) {
	if err := c.serving(ctx, userID); err != nil {
		return Subscription{}, err
	}
	if c.cfg.Int("addons.device_month") <= 0 {
		return Subscription{}, ErrAddonOff
	}
	return c.addon(ctx, userID, "device", func(sub *Subscription, t Tariff, now time.Time) (int64, error) {
		if t.Devices <= 0 {
			return 0, ErrUnlimited
		}
		if sub.ExtraDevices >= maxExtraDevices {
			return 0, ErrInvalid
		}
		days := int((sub.ExpiresAt.Sub(now) + day - 1) / day) // неполный день считается целым
		sub.ExtraDevices++
		return devicesCost(c, 1, days), nil
	})
}

// addon это общая часть докупки: живая платная подписка, цена списывается, панель догоняет через очередь.
func (c *Core) addon(ctx context.Context, userID int64, kind string,
	apply func(sub *Subscription, t Tariff, now time.Time) (int64, error)) (Subscription, error) {
	var out Subscription
	err := c.tx(ctx, func(tx Tx) error {
		now := c.clock.Now()
		if _, err := activeUser(tx, userID); err != nil {
			return err
		}
		sub, err := tx.SubByUser(userID)
		if err != nil || sub.Trial || sub.Imported || sub.Status != SubActive || !sub.ExpiresAt.After(now) {
			return ErrNoSub
		}
		t, err := tx.Tariff(sub.TariffID)
		if err != nil {
			return err
		}
		price, err := apply(&sub, t, now)
		if err != nil {
			return err
		}
		if price > 0 {
			if _, err := move(tx, now, LedgerEntry{UserID: userID, Amount: -price, Reason: ReasonBuy, RefType: kind, Comment: t.Name}); err != nil {
				return needMoney(tx, userID, price, err)
			}
		}
		sub.UpdatedAt = now
		if _, err := tx.SaveSub(sub); err != nil {
			return err
		}
		out = sub
		return enqueueSync(tx, sub.ID, now)
	})
	return out, err
}

// TrafficPack это пакет трафика: гигабайты и цена в копейках.
type TrafficPack struct {
	GB    int64
	Price int64
}

// AddonOffer это то, что абонент может докупить сейчас. Пусто: докупать нечего.
type AddonOffer struct {
	Traffic     []TrafficPack // пакеты по возрастанию; пусто, если тариф безлимитный или пакетов нет
	DevicePrice int64         // цена устройства до конца периода; 0, если докупить нельзя
}

// Addons считает, что абонент может докупить к своей подписке и почём. Цены те же, что спишут BuyTraffic и BuyDevice.
func (c *Core) Addons(ctx context.Context, userID int64) (AddonOffer, error) {
	var out AddonOffer
	err := c.store.Tx(ctx, func(tx Tx) error {
		now := c.clock.Now()
		sub, err := tx.SubByUser(userID)
		if err != nil || sub.Trial || sub.Imported || sub.Status != SubActive || !sub.ExpiresAt.After(now) {
			return nil
		}
		t, err := tx.Tariff(sub.TariffID)
		if err != nil {
			return err
		}
		if t.TrafficBytes > 0 {
			for gb, price := range trafficPacks(c.cfg.String("addons.traffic")) {
				out.Traffic = append(out.Traffic, TrafficPack{GB: gb, Price: price})
			}
			slices.SortFunc(out.Traffic, func(a, b TrafficPack) int { return int(a.GB - b.GB) })
		}
		if t.Devices > 0 && sub.ExtraDevices < maxExtraDevices {
			out.DevicePrice = devicesCost(c, 1, int((sub.ExpiresAt.Sub(now)+day-1)/day))
		}
		return nil
	})
	return out, err
}
