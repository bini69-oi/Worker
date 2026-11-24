package core

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// recurringDays это срок автоподписки Stars. Другого Telegram не даёт.
const recurringDays = 30

// Refunder это касса, которая умеет возвращать оплату. У Stars возврат делает бот через Telegram.
type Refunder interface {
	Refund(ctx context.Context, p Payment, userTgID int64) error
}

// ErrAlreadyRefunded возвращает Refunder, когда Telegram отвечает, что оплата уже возвращена.
// Тогда возврат считается сделанным: так повтор после сбоя базы доводит отмену зачисления до конца.
var ErrAlreadyRefunded = errors.New("оплата уже возвращена")

// ChargeRecurring проводит очередное списание автоподписки Stars. Сначала платёж и зачисление по реально
// уплаченным звёздам и текущему курсу, отдельной транзакцией: Telegram звёзды уже списал, и деньги не должны
// пропасть ни при какой ошибке продления. Потом продление на 30 дней с баланса. Если продлить нельзя
// (тариф скрыт, абонент сменил тариф, денег по новой цене не хватает), деньги остаются на балансе,
// абонент и админы получают уведомление. Номер списания от Telegram уникален, повтор ничего не делает.
func (c *Core) ChargeRecurring(ctx context.Context, userID, tariffID int64, chargeID string, stars int64) (Subscription, error) {
	amount := stars * c.cfg.Int("stars.rate")
	if chargeID == "" || stars <= 0 || amount <= 0 {
		return Subscription{}, ErrInvalid
	}
	err := c.tx(ctx, func(tx Tx) error {
		now := c.clock.Now()
		p := Payment{UserID: userID, Gateway: "stars", Amount: amount, ExtAmount: stars, ExtCurrency: "XTR",
			Recurring: true, Status: PayPending, CreatedAt: now}
		var err error
		if p.ID, err = tx.CreatePayment(p); err != nil {
			return err
		}
		if ok, err := tx.MarkPaid(p.ID, chargeID, now); err != nil || !ok {
			return err
		}
		if _, err := move(tx, now, LedgerEntry{UserID: userID, Amount: amount, Reason: ReasonTopup,
			RefType: "payment", RefID: p.ID}); err != nil {
			return err
		}
		return enqueueAdmin(tx, AdminTask{Topic: TopicPayments, Key: "admin.payment_recurring",
			Vars: map[string]string{"user_id": itoa(userID), "stars": itoa(stars), "amount": itoa(amount)}}, now)
	})
	if errors.Is(err, ErrDuplicate) {
		// Это списание уже проведено. Подписки может и не быть, если продлить тогда не вышло.
		sub, serr := c.store.SubByUser(ctx, userID)
		if errors.Is(serr, ErrNotFound) {
			return Subscription{}, nil
		}
		return sub, serr
	}
	if err != nil {
		return Subscription{}, err
	}

	var sub Subscription
	err = c.tx(ctx, func(tx Tx) error {
		var err error
		sub, err = c.buyTx(tx, userID, tariffID, recurringDays, c.clock.Now())
		return err
	})
	if err != nil {
		c.log.Warn("автоподписка: деньги зачислены, продлить не вышло", "user", userID, "tariff", tariffID, "err", err)
		_ = c.tx(ctx, func(tx Tx) error {
			now := c.clock.Now()
			if err := enqueueNotify(tx, NotifyTask{UserID: userID, Key: "notify.recurring_failed",
				Vars: map[string]string{"amount": itoa(amount)}}, now); err != nil {
				return err
			}
			return enqueueAdmin(tx, AdminTask{Topic: TopicErrors, Key: "admin.recurring_failed",
				Vars: map[string]string{"user_id": itoa(userID), "tariff": itoa(tariffID)}}, now)
		})
		return Subscription{}, err
	}
	return sub, nil
}

// RefundStars возвращает оплату Stars. Сначала возврат в Telegram, и только после его подтверждения
// отменяется зачисление: со счёта абонента списываются деньги, а если их не хватает, срок подписки.
// Если Telegram отказал, ничего не меняется, а ошибка несёт причину.
func (c *Core) RefundStars(ctx context.Context, adminID, paymentID int64) error {
	if err := c.ownerOnly(ctx, adminID, "refund"); err != nil {
		return err
	}
	var p Payment
	var u User
	if err := c.store.Tx(ctx, func(tx Tx) error {
		var err error
		if p, err = tx.Payment(paymentID); err != nil {
			return err
		}
		u, err = tx.User(p.UserID)
		return err
	}); err != nil {
		return err
	}
	if p.Gateway != "stars" || p.Status != PayPaid {
		return ErrInvalid
	}
	g, ok := c.gateway("stars")
	r, can := g.(Refunder)
	if !ok || !can {
		return ErrNoGateway
	}
	if err := r.Refund(ctx, p, u.TgID); err != nil && !errors.Is(err, ErrAlreadyRefunded) {
		return fmt.Errorf("%w: %v", ErrRefundFailed, err)
	}
	err := c.tx(ctx, func(tx Tx) error {
		now := c.clock.Now()
		if err := c.undoPayment(tx, p, now); err != nil {
			return err
		}
		return c.audit(ctx, tx, adminID, "payment.refund", itoa(p.ID), map[string]string{"amount": itoa(p.Amount)}, now)
	})
	if err != nil {
		c.log.Error("возврат прошёл в Telegram, но не отменён в базе", "payment", p.ID, "err", err)
	}
	return err
}

// undoPayment отменяет зачисление платежа после возврата.
func (c *Core) undoPayment(tx Tx, p Payment, now time.Time) error {
	ok, err := tx.SetPaymentStatus(p.ID, PayPaid, PayRefunded)
	if err != nil || !ok {
		return err
	}
	u, err := tx.User(p.UserID)
	if err != nil {
		return err
	}
	take := min(u.Balance, p.Amount)
	if take > 0 {
		if _, err := move(tx, now, LedgerEntry{UserID: p.UserID, Amount: -take, Reason: ReasonRefund,
			RefType: "refund", RefID: p.ID}); err != nil {
			return err
		}
	}
	rest := p.Amount - take
	if rest <= 0 {
		return nil
	}
	sub, err := tx.SubByUser(p.UserID)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if sub.Trial || sub.Price <= 0 || sub.PeriodDays <= 0 {
		return nil
	}
	// Срезаем столько дней, сколько стоит невозвращённый остаток по цене подписки.
	cut := (rest*int64(sub.PeriodDays) + sub.Price - 1) / sub.Price
	sub.ExpiresAt = sub.ExpiresAt.Add(-time.Duration(cut) * day)
	if sub.ExpiresAt.Before(now) {
		sub.ExpiresAt = now
	}
	sub.UpdatedAt = now
	if _, err := tx.SaveSub(sub); err != nil {
		return err
	}
	return enqueueSync(tx, sub.ID, now)
}
