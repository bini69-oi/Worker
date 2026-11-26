package bot

import (
	"context"

	"github.com/bini69-oi/worker/tg"
)

// onPreCheckout отвечает Telegram, можно ли проводить оплату Stars.
func (b *Bot) onPreCheckout(ctx context.Context, q *tg.PreCheckoutQuery) error {
	return b.api.AnswerPreCheckoutQuery(ctx, q.ID, false, "")
}

// onPayment разбирает сообщения об оплате и возврате Stars.
func (b *Bot) onPayment(ctx context.Context, m *tg.Message) error { return nil }
