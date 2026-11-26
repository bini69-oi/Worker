package bot

import (
	"context"
	"errors"
	"strconv"

	"github.com/bini69-oi/worker/core"
	"github.com/bini69-oi/worker/tg"
)

// onPreCheckout отвечает Telegram, можно ли проводить оплату Stars. «Да» только если платёж наш, этого абонента,
// ещё ждёт оплаты и сумма та. Автоподписку проверяем по тарифу: цена считается при списании.
func (b *Bot) onPreCheckout(ctx context.Context, q *tg.PreCheckoutQuery) error {
	r := &req{lang: b.lang(q.From.LanguageCode)}
	kind, id, ok := parsePayload(q.InvoicePayload)
	if !ok || q.Currency != "XTR" {
		return b.api.AnswerPreCheckoutQuery(ctx, q.ID, false, b.t(r, "stars.refused", nil))
	}
	u, err := b.core.UserByTg(ctx, q.From.ID)
	if err != nil {
		return b.api.AnswerPreCheckoutQuery(ctx, q.ID, false, b.t(r, "stars.refused", nil))
	}
	r.lang = u.Lang
	switch kind {
	case "p":
		err = b.core.CheckPayment(ctx, u.ID, "stars", id, q.TotalAmount)
	case "s":
		_, err = b.core.Tariff(ctx, id)
	}
	if err != nil || !u.BannedAt.IsZero() {
		return b.api.AnswerPreCheckoutQuery(ctx, q.ID, false, b.t(r, "stars.refused", nil))
	}
	return b.api.AnswerPreCheckoutQuery(ctx, q.ID, true, "")
}

// onPayment разбирает сообщение об оплате или возврате Stars. Деньги считает ядро, повтор оно отсекает.
func (b *Bot) onPayment(ctx context.Context, m *tg.Message) error {
	if rp := m.RefundedPayment; rp != nil {
		if rp.Currency != "XTR" {
			return b.wrongCurrency(ctx, m.From.ID, rp.Currency)
		}
		err := b.core.Credit(ctx, "stars", core.GatewayEvent{ExternalID: rp.TelegramPaymentChargeID, Status: core.PayRefunded})
		if errors.Is(err, core.ErrNotFound) {
			return nil // возврат платежа, которого у нас нет: нечего отменять
		}
		return err
	}
	sp := m.SuccessfulPayment
	if sp.Currency != "XTR" {
		return b.wrongCurrency(ctx, m.From.ID, sp.Currency)
	}
	u, err := b.core.UserByTg(ctx, m.From.ID)
	if err != nil {
		return err
	}
	kind, id, ok := parsePayload(sp.InvoicePayload)
	if !ok {
		return core.ErrInvalid
	}
	if kind == "s" {
		_, err := b.core.ChargeRecurring(ctx, u.ID, id, sp.TelegramPaymentChargeID, sp.TotalAmount)
		return err
	}
	return b.core.CreditOwn(ctx, u.ID, "stars", core.GatewayEvent{PaymentID: id, ExternalID: sp.TelegramPaymentChargeID,
		ExtAmount: sp.TotalAmount, Status: core.PayPaid})
}

// wrongCurrency сообщает админам об оплате не в звёздах: такого быть не должно, зачислять нельзя.
func (b *Bot) wrongCurrency(ctx context.Context, tgID int64, currency string) error {
	return b.core.ReportError(ctx, "admin.wrong_currency", map[string]string{"tg_id": strconv.FormatInt(tgID, 10), "currency": currency})
}

// onStarsSub выставляет ссылку на автоподписку Stars: каждые 30 дней Telegram сам списывает звёзды
// за 30-дневный срок тарифа, ядро продлевает подписку.
func (b *Bot) onStarsSub(ctx context.Context, r *req, d Data) error {
	id, ok := d.Int(0)
	if !ok {
		return core.ErrInvalid
	}
	t, err := b.core.Tariff(ctx, id)
	if err != nil {
		return err
	}
	stars := b.core.StarsFor30(t)
	if stars <= 0 {
		return core.ErrInvalid
	}
	vars := map[string]string{"tariff": t.Name}
	link, err := b.api.CreateInvoiceLink(ctx, tg.InvoiceParams{Title: b.t(r, "stars.sub_title", vars),
		Description: b.t(r, "stars.sub_desc", vars), Payload: subPayload(t.ID), Currency: "XTR",
		Prices: []tg.LabeledPrice{{Label: t.Name, Amount: stars}}, SubscriptionPeriod: 30 * 24 * 3600})
	if err != nil {
		return err
	}
	return b.show(ctx, r, Screen{Text: b.t(r, "stars.sub_ready", vars), Rows: [][]Button{
		{{Text: b.t(r, "btn.subscribe", nil), URL: link}}, b.back(r, data("bt", t.ID))}})
}
