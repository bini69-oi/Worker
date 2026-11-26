package bot

import (
	"context"
	"errors"

	"github.com/bini69-oi/worker/tg"
)

// Screen это один экран бота: текст и кнопки рядами. Экран собирает функция без побочных действий,
// а показывает его show. Так экран легко сравнить с эталоном в тесте.
type Screen struct {
	Text string
	Rows [][]Button
}

// Button это кнопка экрана. Заполнено ровно одно из Data, URL, Copy, WebApp.
type Button struct {
	Text   string
	Data   string // нажатие возвращается боту
	URL    string // ссылка
	Copy   string // текст в буфер обмена
	WebApp string // адрес Mini App
}

func (s Screen) markup() *tg.InlineKeyboardMarkup {
	if len(s.Rows) == 0 {
		return nil
	}
	kb := &tg.InlineKeyboardMarkup{InlineKeyboard: make([][]tg.InlineKeyboardButton, 0, len(s.Rows))}
	for _, row := range s.Rows {
		out := make([]tg.InlineKeyboardButton, 0, len(row))
		for _, b := range row {
			btn := tg.InlineKeyboardButton{Text: b.Text, CallbackData: b.Data, URL: b.URL}
			if b.Copy != "" {
				btn.CopyText = &tg.CopyTextButton{Text: b.Copy}
			}
			if b.WebApp != "" {
				btn.WebApp = &tg.WebAppInfo{URL: b.WebApp}
			}
			out = append(out, btn)
		}
		kb.InlineKeyboard = append(kb.InlineKeyboard, out)
	}
	return kb
}

// show показывает экран. Нажатие кнопки правит сообщение, в котором она была. Если править нельзя
// (сообщение удалено или старше 48 часов) или экран открыт командой, бот шлёт новое сообщение
// и запоминает его как сообщение-экран абонента.
func (b *Bot) show(ctx context.Context, r *req, s Screen) error {
	if r.msgID != 0 {
		err := b.api.EditMessageText(ctx, tg.EditParams{ChatID: r.chatID, MessageID: r.msgID, Text: s.Text,
			ParseMode: "HTML", Markup: s.markup(), DisablePreview: true})
		var te *tg.Error
		switch {
		case err == nil, errors.As(err, &te) && te.IsNotModified():
			return nil
		case !errors.As(err, &te) || !te.IsNotFound():
			return err
		}
	}
	m, err := b.api.SendMessage(ctx, tg.SendParams{ChatID: r.chatID, Text: s.Text, ParseMode: "HTML",
		Markup: s.markup(), DisablePreview: true})
	if err != nil {
		return err
	}
	r.msgID = m.MessageID
	if r.user.ID != 0 {
		return b.core.SetScreen(ctx, r.user.ID, int64(m.MessageID))
	}
	return nil
}

// say шлёт отдельное короткое сообщение, не трогая экран.
func (b *Bot) say(ctx context.Context, chatID int64, text string) error {
	_, err := b.api.SendMessage(ctx, tg.SendParams{ChatID: chatID, Text: text, ParseMode: "HTML", DisablePreview: true})
	return err
}
