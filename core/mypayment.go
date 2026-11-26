package core

import (
	"context"
	"time"
)

// checkEvery это как часто абонент может просить сверить платёж с кассой: кнопка и опрос кабинета
// не должны долбить API кассы.
const checkEvery = 3 * time.Second

// MyPayment отдаёт платёж абонента. Чужой платёж не отдаётся: ErrNotFound, как будто его нет.
// Ожидающий платёж сверяется с кассой не чаще раза в 3 секунды: вебхук мог задержаться.
func (c *Core) MyPayment(ctx context.Context, userID, paymentID int64) (Payment, error) {
	p, err := c.readPayment(ctx, userID, paymentID)
	if err != nil || p.Status != PayPending || p.ExternalID == "" {
		return p, err
	}
	g, ok := c.gateway(p.Gateway)
	if !ok || !c.mayCheck(ctx, paymentID) {
		return p, nil
	}
	ev, err := g.Status(ctx, p)
	if err != nil {
		c.log.Warn("касса не ответила на проверку", "gateway", p.Gateway, "payment", p.ID, "err", err)
		return p, nil
	}
	if ev.Status == PayPaid {
		ev.PaymentID = p.ID
		if err := c.Credit(ctx, p.Gateway, ev); err != nil {
			return p, err
		}
	}
	return c.readPayment(ctx, userID, paymentID)
}

func (c *Core) readPayment(ctx context.Context, userID, paymentID int64) (Payment, error) {
	var p Payment
	err := c.store.Tx(ctx, func(tx Tx) error {
		var err error
		p, err = tx.Payment(paymentID)
		if err == nil && p.UserID != userID {
			return ErrNotFound
		}
		return err
	})
	return p, err
}

// mayCheck пропускает одну сверку платежа за checkEvery.
func (c *Core) mayCheck(ctx context.Context, paymentID int64) bool {
	ok := false
	_ = c.tx(ctx, func(tx Tx) error {
		now := c.clock.Now()
		key := "paycheck:" + itoa(paymentID)
		_, until, err := tx.Limit(key)
		if err != nil || now.Before(until) {
			return err
		}
		ok = true
		return tx.SetLimit(key, 1, now.Add(checkEvery))
	})
	return ok
}
