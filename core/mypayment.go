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

// FindPending ищет ожидающий платёж кассы по абоненту и сумме. Нужен кассам, которые не знают нашего номера
// платежа (донат Tribute): абонент сам вводит сумму на странице кассы. Платёж с этим номером в кассе уже есть:
// отдаём его, это повтор уведомления. Иначе самый старый ожидающий с точно той же суммой. Нет: ErrNotFound.
func (c *Core) FindPending(ctx context.Context, gateway string, tgID, extAmount int64, externalID string) (Payment, error) {
	var found Payment
	err := c.store.Tx(ctx, func(tx Tx) error {
		if externalID == "" {
			return nil
		}
		p, err := tx.PaymentByExt(gateway, externalID)
		if err == nil {
			found = p
		}
		return nil
	})
	if err != nil || found.ID != 0 {
		return found, err
	}
	u, err := c.store.UserByTg(ctx, tgID)
	if err != nil {
		return Payment{}, err
	}
	list, err := c.store.PendingPayments(ctx, c.clock.Now().Add(time.Second))
	if err != nil {
		return Payment{}, err
	}
	for _, p := range list { // список идёт по возрастанию номера: первый подходящий самый старый
		if p.Gateway == gateway && p.UserID == u.ID && p.ExtAmount == extAmount {
			return p, nil
		}
	}
	return Payment{}, ErrNotFound
}
