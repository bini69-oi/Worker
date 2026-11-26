package bot

import (
	"context"

	"github.com/bini69-oi/worker/tg"
)

// toSupport отправляет текст абонента в поддержку.
func (b *Bot) toSupport(ctx context.Context, r *req, m *tg.Message) error {
	return b.say(ctx, r.chatID, b.t(r, "support.contact", map[string]string{"contact": b.cfg.String("support.contact")}))
}

// fromSupportChat разбирает сообщения в группе поддержки: реплаи админов уходят абонентам.
func (b *Bot) fromSupportChat(ctx context.Context, m *tg.Message) error { return nil }
