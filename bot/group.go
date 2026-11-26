package bot

import (
	"context"

	"github.com/bini69-oi/worker/tg"
)

// onGroup разбирает сообщения в группах: привязка админ-группы и ответы поддержки.
func (b *Bot) onGroup(ctx context.Context, m *tg.Message) error { return nil }

// onMember разбирает добавление бота в группу.
func (b *Bot) onMember(ctx context.Context, u *tg.ChatMemberUpdated) error { return nil }

// adminAct говорит, что кнопка админская: подтверждения, коды входа, действия под событиями.
func adminAct(act string) bool { return act == "cf" || act == "lg" || act == "adm" }

// onAdminCallback разбирает нажатия админов.
func (b *Bot) onAdminCallback(ctx context.Context, q *tg.CallbackQuery, d Data) error {
	return b.api.AnswerCallbackQuery(ctx, q.ID, "", false)
}
