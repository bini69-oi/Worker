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

// ChargeRecurring проводит очередное списание автоподписки Stars: платёж, зачисление и продление
// на 30 дней одной транзакцией. Номер списания от Telegram уникален, поэтому повтор ничего не делает.
// Если абонент отменил автоподписку в Telegram, списаний просто нет и подписка доживает свой срок.
func (c *Core) ChargeRecurring(ctx context.Context, userID, tariffID int64, chargeID string, stars int64) (Subscription, error) {
	if chargeID == "" || stars <= 0 {
		return Subscription{}, ErrInvalid
	}
	var out Subscription
	err := c.tx(ctx, func(tx Tx) error {
		t, err := tx.Tariff(tariffID)
		if err != nil {
			return err
		}
		period, ok := t.period(recurringDays)
		if !ok {
			return ErrNoPeriod
		}
		now := c.clock.Now()
		p := Payment{UserID: userID, Gateway: "stars", Amount: period.Price, ExtAmount: stars, ExtCurrency: "XTR",
			Recurring: true, Status: PayPending, CreatedAt: now}
		if p.ID, err = tx.CreatePayment(p); err != nil {
			return err
		}
		if ok, err := tx.MarkPaid(p.ID, chargeID, now); err != nil || !ok {
			return err
		}
		if period.Price > 0 {
			if _, err := move(tx, now, LedgerEntry{UserID: userID, Amount: period.Price, Reason: ReasonTopup,
				RefType: "payment", RefID: p.ID}); err != nil {
				return err
			}
		}
		if out, err = c.buyTx(tx, userID, tariffID, recurringDays, now); err != nil {
			return err
		}
		return enqueueAdmin(tx, AdminTask{Topic: TopicPayments, Key: "admin.payment_recurring",
			Vars: map[string]string{"user_id": fmt.Sprint(userID), "stars": fmt.Sprint(stars)}}, now)
	})
	if errors.Is(err, ErrDuplicate) {
		return c.store.SubByUser(ctx, userID) // это списание уже проведено
	}
	return out, err
}

// RefundStars возвращает оплату Stars. Сначала возврат в Telegram, и только после его подтверждения
// отменяется зачисление: со счёта абонента списываются деньги, а если их не хватает, срок подписки.
// Если Telegram отказал, ничего не меняется, а ошибка несёт причину.
func (c *Core) RefundStars(ctx context.Context, adminID, paymentID int64) error {
	if adminID <= 0 {
		return ErrForbidden
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
	if err := r.Refund(ctx, p, u.TgID); err != nil {
		return fmt.Errorf("%w: %v", ErrRefundFailed, err)
	}
	// Запись в журнал действий админов встанет сюда (группа 11).
	err := c.tx(ctx, func(tx Tx) error { return c.undoPayment(tx, p, c.clock.Now()) })
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
	if errors.Is(err, ErrNotFound) || sub.Trial || sub.Price <= 0 || sub.PeriodDays <= 0 {
		return nil
	}
	if err != nil {
		return err
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
