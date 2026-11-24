package core

import (
	"context"
	"errors"
	"sort"
	"strconv"
	"time"
)

// Invoice это счёт от кассы: ссылка на оплату или данные для счёта в Telegram.
type Invoice struct {
	ExternalID  string // номер платежа в кассе, если касса выдала его сразу
	URL         string // ссылка на оплату, пустая у Stars
	ExtAmount   int64  // сумма в валюте кассы
	ExtCurrency string
	Text        string // подсказка абоненту, например реквизиты у ручного зачисления
}

// GatewayEvent это проверенное сообщение кассы об оплате.
type GatewayEvent struct {
	PaymentID  int64  // наш номер платежа, если касса его вернула
	ExternalID string // номер платежа в кассе
	ExtAmount  int64  // сколько оплачено в валюте кассы
	Status     PayStatus
}

// Gateway это касса глазами ядра. Каждая касса лежит своим файлом в пакете pay.
type Gateway interface {
	Name() string
	// Configured говорит, что ключи кассы заполнены. Касса без ключей абоненту не видна.
	Configured() bool
	Create(ctx context.Context, p Payment) (Invoice, error)
	// Status спрашивает кассу о платеже, если вебхук не пришёл.
	Status(ctx context.Context, p Payment) (GatewayEvent, error)
}

// GatewayInfo это касса в списке для абонента.
type GatewayInfo struct {
	Name  string
	Title string
}

// LimitError это «сумма вне пределов кассы» с самими пределами.
type LimitError struct {
	Min, Max int64 // копейки, 0 значит без предела
}

func (e *LimitError) Error() string { return ErrAmountLimits.Error() }
func (e *LimitError) Unwrap() error { return ErrAmountLimits }

// Gateways отдаёт кассы, которые видит абонент: включены, ключи заполнены. Порядок из настроек.
func (c *Core) Gateways() []GatewayInfo {
	var out []GatewayInfo
	for _, g := range c.gateways {
		if c.visible(g) {
			out = append(out, GatewayInfo{Name: g.Name(), Title: c.cfg.String(g.Name() + ".title")})
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		return c.cfg.Int(out[i].Name+".sort") < c.cfg.Int(out[j].Name+".sort")
	})
	return out
}

func (c *Core) visible(g Gateway) bool {
	return c.cfg.Bool(g.Name()+".enabled") && g.Configured()
}

func (c *Core) gateway(name string) (Gateway, bool) {
	for _, g := range c.gateways {
		if g.Name() == name {
			return g, true
		}
	}
	return nil, false
}

// CreatePayment создаёт платёж «ожидает» и счёт в кассе.
func (c *Core) CreatePayment(ctx context.Context, userID int64, gateway string, amount int64) (Payment, Invoice, error) {
	g, ok := c.gateway(gateway)
	if !ok || !c.visible(g) {
		return Payment{}, Invoice{}, ErrNoGateway
	}
	min, max := c.cfg.Int(gateway+".min"), c.cfg.Int(gateway+".max")
	if amount <= 0 || (min > 0 && amount < min) || (max > 0 && amount > max) {
		return Payment{}, Invoice{}, &LimitError{Min: min, Max: max}
	}
	p := Payment{UserID: userID, Gateway: gateway, Amount: amount, Status: PayPending, CreatedAt: c.clock.Now()}
	err := c.tx(ctx, func(tx Tx) error {
		if _, err := activeUser(tx, userID); err != nil {
			return err
		}
		var err error
		p.ID, err = tx.CreatePayment(p)
		return err
	})
	if err != nil {
		return p, Invoice{}, err
	}

	inv, err := g.Create(ctx, p)
	if err != nil {
		c.log.Warn("касса не создала счёт", "gateway", gateway, "payment", p.ID, "err", err)
		_ = c.tx(ctx, func(tx Tx) error { _, err := tx.SetPaymentStatus(p.ID, PayPending, PayFailed); return err })
		return p, Invoice{}, ErrGatewayDown
	}
	p.ExternalID, p.ExtAmount, p.ExtCurrency = inv.ExternalID, inv.ExtAmount, inv.ExtCurrency
	err = c.tx(ctx, func(tx Tx) error { return tx.SetPaymentExt(p.ID, p.ExternalID, p.ExtAmount, p.ExtCurrency) })
	return p, inv, err
}

// Credit зачисляет оплату ровно один раз. Повтор вебхука и одновременная сверка ничего не добавят:
// платёж переводится в «оплачен» условным обновлением, а строка истории денег уникальна для платежа.
func (c *Core) Credit(ctx context.Context, gateway string, ev GatewayEvent) error {
	if ev.Status != PayPaid {
		return nil
	}
	var credited Payment
	err := c.tx(ctx, func(tx Tx) error {
		p, err := findPayment(tx, gateway, ev)
		if err != nil {
			return err
		}
		if p.Status == PayPaid {
			return nil
		}
		if p.ExtAmount != 0 && ev.ExtAmount != p.ExtAmount {
			c.log.Warn("сумма оплаты не совпала", "payment", p.ID, "want", p.ExtAmount, "got", ev.ExtAmount)
			return ErrAmountMismatch
		}
		now := c.clock.Now()
		ok, err := tx.MarkPaid(p.ID, ev.ExternalID, now)
		if err != nil || !ok {
			return err
		}
		if _, err := move(tx, now, LedgerEntry{UserID: p.UserID, Amount: p.Amount, Reason: ReasonTopup,
			RefType: "payment", RefID: p.ID}); err != nil {
			return err
		}
		credited = p
		return c.afterTopup(tx, p, now)
	})
	if errors.Is(err, ErrDuplicate) {
		return nil // зачисление уже было, второй вебхук ничего не меняет
	}
	if err == nil && credited.ID != 0 {
		c.log.Info("платёж зачислен", "payment", credited.ID, "gateway", gateway, "amount", credited.Amount)
	}
	return err
}

func findPayment(tx Tx, gateway string, ev GatewayEvent) (Payment, error) {
	if ev.PaymentID != 0 {
		p, err := tx.Payment(ev.PaymentID)
		if err == nil && p.Gateway != gateway {
			return p, ErrNotFound
		}
		return p, err
	}
	if ev.ExternalID == "" {
		return Payment{}, ErrNotFound
	}
	return tx.PaymentByExt(gateway, ev.ExternalID)
}

// afterTopup сообщает абоненту и админам о пополнении. Покупку из умной корзины добавит группа 8.
func (c *Core) afterTopup(tx Tx, p Payment, now time.Time) error {
	amount := strconv.FormatInt(p.Amount, 10)
	if err := enqueueNotify(tx, NotifyTask{UserID: p.UserID, Key: "notify.topup", Vars: map[string]string{"amount": amount}}, now); err != nil {
		return err
	}
	return enqueueAdmin(tx, AdminTask{Topic: TopicPayments, Key: "admin.payment",
		Vars: map[string]string{"user_id": strconv.FormatInt(p.UserID, 10), "amount": amount, "gateway": p.Gateway}}, now)
}

// ReconcilePayments сверяет зависшие платежи с кассами: оплаченный зачисляется, просроченный помечается «истёк».
// Срок жизни платежа в часах берётся из настройки pay.pending_hours.
func (c *Core) ReconcilePayments(ctx context.Context) (int, error) {
	now := c.clock.Now()
	list, err := c.store.PendingPayments(ctx, now.Add(-time.Minute))
	if err != nil {
		return 0, err
	}
	ttl := time.Duration(c.cfg.Int("pay.pending_hours")) * time.Hour
	credited := 0
	for _, p := range list {
		g, ok := c.gateway(p.Gateway)
		if ok && p.ExternalID != "" {
			ev, err := g.Status(ctx, p)
			if err != nil {
				c.log.Warn("касса не ответила на сверку", "gateway", p.Gateway, "payment", p.ID, "err", err)
			} else if ev.Status == PayPaid {
				ev.PaymentID = p.ID
				if err := c.Credit(ctx, p.Gateway, ev); err != nil {
					c.log.Warn("сверка не зачислила", "payment", p.ID, "err", err)
					continue
				}
				credited++
				continue
			}
		}
		if ttl > 0 && now.Sub(p.CreatedAt) > ttl {
			if err := c.tx(ctx, func(tx Tx) error { _, err := tx.SetPaymentStatus(p.ID, PayPending, PayExpired); return err }); err != nil {
				return credited, err
			}
		}
	}
	return credited, nil
}

// CheckPayment проверяет платёж перед оплатой, когда касса спрашивает «можно?» (pre_checkout_query у Stars):
// платёж наш, ещё ожидает и сумма та.
func (c *Core) CheckPayment(ctx context.Context, gateway string, paymentID, extAmount int64) error {
	var p Payment
	err := c.store.Tx(ctx, func(tx Tx) error {
		var err error
		p, err = tx.Payment(paymentID)
		return err
	})
	if err != nil {
		return err
	}
	if p.Gateway != gateway || p.Status != PayPending {
		return ErrNotFound
	}
	if p.ExtAmount != extAmount {
		return ErrAmountMismatch
	}
	return nil
}

// ConfirmManual подтверждает ручной платёж от имени админа: деньги пришли на реквизиты.
// Работает только для ручной кассы, платежи настоящих касс зачисляет сама касса.
func (c *Core) ConfirmManual(ctx context.Context, adminID, paymentID int64) error {
	if adminID <= 0 {
		return ErrForbidden
	}
	var p Payment
	if err := c.store.Tx(ctx, func(tx Tx) error {
		var err error
		p, err = tx.Payment(paymentID)
		return err
	}); err != nil {
		return err
	}
	if p.Gateway != "manual" {
		return ErrForbidden
	}
	// Запись в журнал действий админов встанет сюда (группа 11).
	return c.Credit(ctx, "manual", GatewayEvent{PaymentID: p.ID, ExtAmount: p.ExtAmount, Status: PayPaid})
}
