package core

import (
	"context"
	"errors"
	"regexp"
	"time"
)

// Ключ повтора: страница создаёт случайный ключ на одно действие человека и шлёт его же при повторе после потерянного ответа.
// Ядро помнит ключ в той же транзакции, что и списание, и второй запрос с ним ничего не списывает.
var requestKeyRe = regexp.MustCompile(`^[A-Za-z0-9_-]{16,64}$`)

// CheckRequestKey проверяет ключ повтора на границе: длина и алфавит.
func CheckRequestKey(key string) error {
	if !requestKeyRe.MatchString(key) {
		return ErrInvalid
	}
	return nil
}

// errReplayed откатывает транзакцию, в которой действие с этим ключом уже было сделано раньше.
var errReplayed = errors.New("запрос с этим ключом уже выполнен")

// requestKinds это виды отметок о ключах. Их стирает CleanCabinet через сутки.
const requestPrefix = "req_"

// seen говорит, что действие с этим ключом уже сделано: тогда его не повторяют, а отдают текущее состояние.
// Проверка идёт до действия, чтобы повтор не упёрся в «не хватает денег» или «смены нет» и не запомнил корзину.
func seen(tx Tx, userID int64, kind, key string) error {
	if key == "" {
		return nil
	}
	ok, err := tx.HasNotice(userID, requestPrefix+kind, key)
	if err != nil {
		return err
	}
	if ok {
		return errReplayed
	}
	return nil
}

// claim запоминает ключ повтора после того, как действие выполнено в этой же транзакции. Ключ уже был: errReplayed,
// и транзакция откатывается вместе с только что сделанным действием. Пустой ключ ничего не проверяет.
func claim(tx Tx, userID int64, kind, key string, now time.Time) error {
	if key == "" {
		return nil
	}
	first, err := tx.AddNotice(userID, requestPrefix+kind, key, now)
	if err != nil {
		return err
	}
	if !first {
		return errReplayed
	}
	return nil
}

// Shortfall это недостача при покупке: цена, баланс и сколько не хватает, копейки.
type Shortfall struct {
	Price, Balance, Missing int64
}

// BuyOutcome это итог покупки. Short не nil: денег не хватило, ничего не куплено, выбор запомнен до пополнения.
// Replayed: этот ключ уже покупал, списания нет, Sub это текущая подписка.
type BuyOutcome struct {
	Sub      Subscription
	Queued   bool
	Replayed bool
	Short    *Shortfall
}

// BuyOrRemember покупает тариф по ключу повтора. Не хватает денег: в той же транзакции запоминает выбор, после
// пополнения подписка выдастся сама, и отдаёт недостачу. Повтор с тем же ключом ничего не списывает.
func (c *Core) BuyOrRemember(ctx context.Context, userID, tariffID int64, days int, key string) (BuyOutcome, error) {
	if err := CheckRequestKey(key); err != nil {
		return BuyOutcome{}, err
	}
	if err := c.serving(ctx, userID); err != nil {
		return BuyOutcome{}, err
	}
	var out BuyOutcome
	err := c.tx(ctx, func(tx Tx) error {
		now := c.clock.Now()
		if err := seen(tx, userID, "buy", key); err != nil {
			return err
		}
		sub, err := c.buyTx(tx, userID, tariffID, days, now)
		var need *NeedMoney
		if errors.As(err, &need) {
			// Деньги не списаны и ничего не записано: выбор запоминается и транзакция завершается успешно.
			out.Short = &Shortfall{Price: need.Price, Balance: need.Balance, Missing: need.Missing}
			return tx.SetPendingBuy(PendingBuy{UserID: userID, TariffID: tariffID, Days: days, CreatedAt: now})
		}
		if err != nil {
			return err
		}
		if err := tx.DeletePendingBuy(userID); err != nil {
			return err
		}
		out.Sub, out.Queued = sub, sub.NextTariffID != 0
		return claim(tx, userID, "buy", key, now)
	})
	return c.replayed(ctx, userID, out, err)
}

// replayed превращает errReplayed в успешный итог: подписка как она есть сейчас.
func (c *Core) replayed(ctx context.Context, userID int64, out BuyOutcome, err error) (BuyOutcome, error) {
	if !errors.Is(err, errReplayed) {
		return out, err
	}
	sub, serr := c.store.SubByUser(ctx, userID)
	return BuyOutcome{Sub: sub, Queued: sub.NextTariffID != 0, Replayed: true}, serr
}

// BuyTrafficOnce докупает пакет трафика по ключу повтора. replayed: ключ уже покупал, списания нет.
func (c *Core) BuyTrafficOnce(ctx context.Context, userID, gb int64, key string) (Subscription, bool, error) {
	if err := CheckRequestKey(key); err != nil {
		return Subscription{}, false, err
	}
	return c.buyTraffic(ctx, userID, gb, key)
}

// BuyDeviceOnce докупает устройство по ключу повтора.
func (c *Core) BuyDeviceOnce(ctx context.Context, userID int64, key string) (Subscription, bool, error) {
	if err := CheckRequestKey(key); err != nil {
		return Subscription{}, false, err
	}
	return c.buyDevice(ctx, userID, key)
}

// CancelPlanOnce отменяет ждущую смену тарифа по ключу повтора: возврат на баланс один раз.
func (c *Core) CancelPlanOnce(ctx context.Context, userID int64, key string) (bool, error) {
	if err := CheckRequestKey(key); err != nil {
		return false, err
	}
	err := c.tx(ctx, func(tx Tx) error {
		if err := seen(tx, userID, "cancel", key); err != nil {
			return err
		}
		sub, err := tx.SubByUser(userID)
		if err != nil {
			return err
		}
		if sub.NextTariffID == 0 {
			return ErrNotFound
		}
		now := c.clock.Now()
		if err := c.dropPlan(tx, sub, now); err != nil {
			return err
		}
		return claim(tx, userID, "cancel", key, now)
	})
	return errors.Is(err, errReplayed), swallowReplay(err)
}

func swallowReplay(err error) error {
	if errors.Is(err, errReplayed) {
		return nil
	}
	return err
}
