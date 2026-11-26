package bot

import (
	"context"

	"github.com/bini69-oi/worker/tg"
)

// API это методы Telegram, которые зовёт бот. Реализует *tg.Client, в тестах стоит фейк, который записывает вызовы.
type API interface {
	SendMessage(ctx context.Context, p tg.SendParams) (tg.Message, error)
	EditMessageText(ctx context.Context, p tg.EditParams) error
	AnswerCallbackQuery(ctx context.Context, id, text string, showAlert bool) error
	CopyMessage(ctx context.Context, toChat int64, thread int, fromChat int64, msgID, replyTo int) (int, error)
	SendInvoice(ctx context.Context, p tg.InvoiceParams) (tg.Message, error)
	CreateInvoiceLink(ctx context.Context, p tg.InvoiceParams) (string, error)
	AnswerPreCheckoutQuery(ctx context.Context, id string, ok bool, errMsg string) error
	SetMyCommands(ctx context.Context, cmds []tg.BotCommand, lang string) error
	SetChatMenuButton(ctx context.Context, chatID int64, text, webAppURL string) error
	CreateForumTopic(ctx context.Context, chat int64, name string) (int, error)
	GetChatMember(ctx context.Context, chat string, user int64) (string, error)
	LeaveChat(ctx context.Context, chat int64) error
}

var _ API = (*tg.Client)(nil)
