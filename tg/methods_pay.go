package tg

import "context"

// InvoiceParams это счёт. Для звёзд Currency "XTR", provider_token не передаётся.
type InvoiceParams struct {
	ChatID             int64 // только для SendInvoice
	ThreadID           int   // только для SendInvoice
	Title              string
	Description        string
	Payload            string
	Currency           string
	Prices             []LabeledPrice
	Markup             *InlineKeyboardMarkup // только для SendInvoice
	SubscriptionPeriod int                   // секунды, только для CreateInvoiceLink (поддерживается 2592000)
}

func (p InvoiceParams) body() map[string]any {
	req := map[string]any{
		"title": p.Title, "description": p.Description, "payload": p.Payload,
		"currency": p.Currency, "prices": p.Prices,
	}
	return req
}

// SendInvoice отправляет счёт в чат.
func (c *Client) SendInvoice(ctx context.Context, p InvoiceParams) (Message, error) {
	req := p.body()
	req["chat_id"] = p.ChatID
	if p.ThreadID != 0 {
		req["message_thread_id"] = p.ThreadID
	}
	if p.Markup != nil {
		req["reply_markup"] = p.Markup
	}
	var m Message
	err := c.call(ctx, "sendInvoice", req, &m)
	return m, err
}

// CreateInvoiceLink создаёт ссылку на счёт, с SubscriptionPeriod это подписка.
func (c *Client) CreateInvoiceLink(ctx context.Context, p InvoiceParams) (string, error) {
	req := p.body()
	if p.SubscriptionPeriod != 0 {
		req["subscription_period"] = p.SubscriptionPeriod
	}
	var link string
	err := c.call(ctx, "createInvoiceLink", req, &link)
	return link, err
}

// AnswerPreCheckoutQuery подтверждает оплату или отказывает с текстом errMsg.
func (c *Client) AnswerPreCheckoutQuery(ctx context.Context, id string, ok bool, errMsg string) error {
	req := map[string]any{"pre_checkout_query_id": id, "ok": ok}
	if !ok {
		req["error_message"] = errMsg
	}
	return c.call(ctx, "answerPreCheckoutQuery", req, nil)
}

// RefundStarPayment возвращает звёзды за платёж.
func (c *Client) RefundStarPayment(ctx context.Context, userID int64, chargeID string) error {
	return c.call(ctx, "refundStarPayment",
		map[string]any{"user_id": userID, "telegram_payment_charge_id": chargeID}, nil)
}

// EditUserStarSubscription отменяет подписку на звёзды или возобновляет её.
func (c *Client) EditUserStarSubscription(ctx context.Context, userID int64, chargeID string, canceled bool) error {
	return c.call(ctx, "editUserStarSubscription", map[string]any{
		"user_id": userID, "telegram_payment_charge_id": chargeID, "is_canceled": canceled,
	}, nil)
}
