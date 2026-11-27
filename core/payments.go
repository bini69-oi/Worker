package core

import (
	"context"
	"errors"
	"fmt"
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

// Предел счетов на абонента: не больше payTries за payWindow.
const (
	payTries  = 5
	payWindow = 10 * time.Minute
)

// CreatePayment создаёт платёж «ожидает» и счёт в кассе.
func (c *Core) CreatePayment(ctx context.Context, userID int64, gateway string, amount int64) (Payment, Invoice, error) {
	if err := c.serving(ctx, userID); err != nil {
		return Payment{}, Invoice{}, err
	}
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
		// Счета в кассе без предела позволяли бы наплодить их тысячи, и касса ограничила бы магазин.
		if err := countTry(tx, "pay:"+itoa(userID), payTries, payWindow, p.CreatedAt); err != nil {
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
// Возврат или оспаривание оплаты (PayRefunded) отменяет зачисление.
func (c *Core) Credit(ctx context.Context, gateway string, ev GatewayEvent) error {
	switch ev.Status {
	case PayPaid:
	case PayRefunded:
		return c.reverse(ctx, gateway, ev)
	default:
		return nil
	}
	var credited, seen Payment
	err := c.tx(ctx, func(tx Tx) error {
		var err error
		credited, seen, err = c.creditTx(tx, gateway, ev, c.clock.Now())
		return err
	})
	if errors.Is(err, ErrDuplicate) {
		return nil // строка зачисления уже есть: второй вебхук ничего не меняет
	}
	if errors.Is(err, ErrAmountMismatch) && seen.ID != 0 {
		c.amountAlert(ctx, seen, ev.ExtAmount)
	}
	if err == nil && credited.ID != 0 {
		c.log.Info("платёж зачислен", "payment", credited.ID, "gateway", gateway, "amount", credited.Amount)
		c.buyFromCart(ctx, credited.UserID)
	}
	return err
}

// creditTx зачисляет платёж внутри транзакции. Отдаёт зачисленный платёж (пустой, если зачислять нечего)
// и найденный платёж, чтобы при неверной сумме было кому прислать тревогу.
func (c *Core) creditTx(tx Tx, gateway string, ev GatewayEvent, now time.Time) (Payment, Payment, error) {
	p, err := findPayment(tx, gateway, ev)
	if err != nil {
		return Payment{}, Payment{}, err
	}
	if p.Status == PayPaid || p.Status == PayRefunded {
		return Payment{}, p, nil
	}
	if err := sameExt(p, ev); err != nil {
		return Payment{}, p, err
	}
	// Сумму без записи о том, сколько ждём, не зачисляем: сверять было бы не с чем.
	if p.ExtAmount <= 0 || ev.ExtAmount != p.ExtAmount {
		c.log.Warn("сумма оплаты не совпала", "payment", p.ID, "want", p.ExtAmount, "got", ev.ExtAmount)
		return Payment{}, p, ErrAmountMismatch
	}
	ok, err := tx.MarkPaid(p.ID, ev.ExternalID, now)
	if errors.Is(err, ErrDuplicate) {
		// Этот номер кассы уже у другого платежа: касса прислала чужой номер. Это не «уже зачислено».
		return Payment{}, p, fmt.Errorf("номер оплаты %q уже у другого платежа: %w", ev.ExternalID, ErrInvalid)
	}
	if err != nil || !ok {
		return Payment{}, p, err
	}
	if _, err := move(tx, now, LedgerEntry{UserID: p.UserID, Amount: p.Amount, Reason: ReasonTopup,
		RefType: "payment", RefID: p.ID}); err != nil {
		return Payment{}, p, err
	}
	if err := c.welcomeBonus(tx, p.UserID, now); err != nil {
		return Payment{}, p, err
	}
	return p, p, c.afterTopup(tx, p, now)
}

// sameExt проверяет, что касса говорит о том же платеже, который она выдала при создании счёта.
// Иначе старое настоящее уведомление можно приложить к новому платежу с тем же нашим номером
// (база откатилась из бэкапа или две копии бота работают на одной кассе).
func sameExt(p Payment, ev GatewayEvent) error {
	if p.ExternalID != "" && ev.ExternalID != "" && ev.ExternalID != p.ExternalID {
		return fmt.Errorf("номер оплаты %q не совпал с номером платежа: %w", ev.ExternalID, ErrInvalid)
	}
	return nil
}

// amountAlert сообщает админам, что пришла оплата не на ту сумму. Касса будет повторять уведомление,
// поэтому тревога уходит один раз на платёж.
func (c *Core) amountAlert(ctx context.Context, p Payment, got int64) {
	now := c.clock.Now()
	_ = c.tx(ctx, func(tx Tx) error {
		first, err := tx.AddNotice(p.UserID, "amount_mismatch", itoa(p.ID), now)
		if err != nil || !first {
			return err
		}
		err = enqueueAdmin(tx, AdminTask{Topic: TopicPayments, Key: "admin.amount_mismatch", Vars: map[string]string{
			"payment": itoa(p.ID), "user_id": itoa(p.UserID), "gateway": p.Gateway,
			"want": itoa(p.ExtAmount), "got": itoa(got)}}, now)
		if err != nil {
			return err
		}
		// Абонент тоже узнаёт, что платёж не потерялся, а ждёт проверки админом.
		return enqueueNotify(tx, NotifyTask{UserID: p.UserID, Key: "notify.pay_review",
			Vars: map[string]string{"payment_id": itoa(p.ID), "gateway": p.Gateway}}, now)
	})
}

// reverse отменяет зачисление, когда касса сообщила о возврате или оспаривании оплаты: деньги снимаются
// с баланса, а чего не хватило, срезается днями подписки. Админы получают сообщение.
func (c *Core) reverse(ctx context.Context, gateway string, ev GatewayEvent) error {
	return c.tx(ctx, func(tx Tx) error {
		p, err := findPayment(tx, gateway, ev)
		if err != nil {
			return err
		}
		if p.Status != PayPaid {
			return nil
		}
		if err := sameExt(p, ev); err != nil {
			return err
		}
		now := c.clock.Now()
		if err := c.undoPayment(tx, p, now); err != nil {
			return err
		}
		return enqueueAdmin(tx, AdminTask{Topic: TopicPayments, Key: "admin.payment_reversed", Vars: map[string]string{
			"payment": itoa(p.ID), "user_id": itoa(p.UserID), "gateway": p.Gateway, "amount": itoa(p.Amount)}}, now)
	})
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

// afterTopup сообщает абоненту и админам о пополнении.
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
			if err := c.tx(ctx, func(tx Tx) error { return c.expirePayment(tx, p, now) }); err != nil {
				return credited, err
			}
		}
	}
	return credited, nil
}

// expirePayment помечает платёж «истёк» и говорит об этом абоненту, чтобы он не ждал зачисления молча.
func (c *Core) expirePayment(tx Tx, p Payment, now time.Time) error {
	changed, err := tx.SetPaymentStatus(p.ID, PayPending, PayExpired)
	if err != nil || !changed {
		return err
	}
	vars := map[string]string{"amount": strconv.FormatInt(p.Amount, 10), "gateway": p.Gateway, "payment_id": strconv.FormatInt(p.ID, 10)}
	return enqueueNotify(tx, NotifyTask{UserID: p.UserID, Key: "notify.pay_expired", Vars: vars}, now)
}

// CheckPayment проверяет платёж перед оплатой, когда касса спрашивает «можно?» (pre_checkout_query у Stars):
// платёж наш, ещё ожидает и сумма та.
func (c *Core) CheckPayment(ctx context.Context, userID int64, gateway string, paymentID, extAmount int64) error {
	var p Payment
	err := c.store.Tx(ctx, func(tx Tx) error {
		var err error
		p, err = tx.Payment(paymentID)
		return err
	})
	if err != nil {
		return err
	}
	if p.UserID != userID || p.Gateway != gateway || p.Status != PayPending {
		return ErrNotFound
	}
	if p.ExtAmount != extAmount {
		return ErrAmountMismatch
	}
	return nil
}

// ConfirmManual подтверждает ручной платёж от имени админа: деньги пришли на реквизиты.
// Работает только для ручной кассы, платежи настоящих касс зачисляет сама касса.
// Право проверяется в той же транзакции, что и зачисление: отключённый помощник не успеет зачислить.
func (c *Core) ConfirmManual(ctx context.Context, adminID, paymentID int64) error {
	err := c.ConfirmPayment(ctx, adminID, paymentID)
	if errors.Is(err, ErrAlreadyPaid) {
		return nil
	}
	return err
}

// ConfirmPayment как ConfirmManual, но на повторное нажатие отвечает ErrAlreadyPaid: админка так говорит
// «уже зачислен». Деньги зачисляются ровно один раз, повтор ничего не меняет и в журнал не пишется.
func (c *Core) ConfirmPayment(ctx context.Context, adminID, paymentID int64) error {
	var credited Payment
	err := c.asAdmin(ctx, adminID, PermGrant, "payment.confirm", func(tx Tx, a Admin, now time.Time) (string, map[string]string, error) {
		p, err := tx.Payment(paymentID)
		if err != nil {
			return "", nil, err
		}
		if p.Gateway != "manual" {
			return "", nil, ErrForbidden
		}
		if credited, _, err = c.creditTx(tx, "manual", GatewayEvent{PaymentID: p.ID, ExtAmount: p.ExtAmount, Status: PayPaid}, now); err != nil {
			return "", nil, err
		}
		if credited.ID == 0 {
			return "", nil, ErrAlreadyPaid
		}
		if err := c.helperMoney(tx, a, "payment.confirm", p.UserID, p.Amount, now); err != nil {
			return "", nil, err
		}
		return itoa(p.ID), map[string]string{"amount": itoa(p.Amount)}, nil
	})
	if errors.Is(err, ErrDuplicate) {
		return ErrAlreadyPaid
	}
	if err == nil {
		c.buyFromCart(ctx, credited.UserID)
	}
	return err
}

// CreditOwn зачисляет оплату, которую абонент прислал сам (Stars через бота): платёж должен быть его.
func (c *Core) CreditOwn(ctx context.Context, userID int64, gateway string, ev GatewayEvent) error {
	if err := c.ownPayment(ctx, userID, ev.PaymentID); err != nil {
		return err
	}
	return c.Credit(ctx, gateway, ev)
}

// ownPayment проверяет, что платёж принадлежит абоненту. Чужой выглядит как несуществующий.
func (c *Core) ownPayment(ctx context.Context, userID, paymentID int64) error {
	return c.store.Tx(ctx, func(tx Tx) error {
		p, err := tx.Payment(paymentID)
		if err != nil || p.UserID != userID {
			return ErrNotFound
		}
		return nil
	})
}
