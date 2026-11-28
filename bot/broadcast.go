package bot

import (
	"context"
	"errors"
	"strings"

	"github.com/bini69-oi/worker/core"
	"github.com/bini69-oi/worker/tg"
)

var _ core.BroadcastSender = (*Bot)(nil)

// Send отправляет одно сообщение рассылки в личку: текст как разметка Telegram HTML (ядро проверило её при
// сохранении) и по желанию одна кнопка-ссылка. Заблокировавший бота даёт core.ErrRecipientGone,
// ответ 429 даёт *core.RetryAfter.
func (b *Bot) Send(ctx context.Context, tgID int64, text, buttonText, buttonURL string) error {
	p := tg.SendParams{ChatID: tgID, Text: text, ParseMode: "HTML", DisablePreview: true}
	if buttonText != "" && buttonURL != "" {
		p.Markup = &tg.InlineKeyboardMarkup{InlineKeyboard: [][]tg.InlineKeyboardButton{{{Text: buttonText, URL: buttonURL}}}}
	}
	_, err := b.api.SendMessage(ctx, p)
	var te *tg.Error
	switch {
	case !errors.As(err, &te):
		return err
	case te.IsBlocked():
		return core.ErrRecipientGone
	case te.Code == 400 && markupRejected(te.Description):
		return core.ErrMarkupRejected
	case te.Code == 429:
		return &core.RetryAfter{Seconds: te.RetryAfter}
	}
	return err
}

// markupRejected узнаёт ответ Telegram об ошибке разбора разметки.
func markupRejected(desc string) bool {
	d := strings.ToLower(desc)
	return strings.Contains(d, "can't parse entities") || strings.Contains(d, "can't find end of")
}
