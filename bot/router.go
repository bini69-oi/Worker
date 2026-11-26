package bot

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/bini69-oi/worker/core"
	"github.com/bini69-oi/worker/tg"
)

// banNoticeEvery это как часто забаненный видит текст о блокировке: не на каждое нажатие.
const banNoticeEvery = time.Hour

// handle разбирает одно обновление по виду.
func (b *Bot) handle(ctx context.Context, u tg.Update) error {
	switch {
	case u.CallbackQuery != nil:
		return b.onCallback(ctx, u.CallbackQuery)
	case u.PreCheckoutQuery != nil:
		return b.onPreCheckout(ctx, u.PreCheckoutQuery)
	case u.Message != nil && u.Message.Chat.Type == "private":
		return b.onPrivate(ctx, u.Message)
	case u.Message != nil:
		return b.onGroup(ctx, u.Message)
	case u.MyChatMember != nil:
		return b.onMember(ctx, u.MyChatMember)
	}
	return nil
}

// onCallback это нажатие кнопки. Нажатия админов в группе и в личке идут своим путём.
func (b *Bot) onCallback(ctx context.Context, q *tg.CallbackQuery) error {
	d, perr := parseData(q.Data)
	if perr == nil && adminAct(d.Act) {
		return b.onAdminCallback(ctx, q, d)
	}
	if q.Message == nil || q.Message.Chat.Type != "private" {
		return b.api.AnswerCallbackQuery(ctx, q.ID, "", false)
	}
	r, ok, err := b.enter(ctx, q.From, q.Message.Chat.ID, q.ID, "")
	if err != nil || !ok {
		return err
	}
	r.msgID = q.Message.MessageID
	b.waits.clear(q.From.ID) // любая кнопка сбрасывает ожидание ввода
	act, known := b.acts[d.Act]
	if perr != nil || !known {
		_ = b.api.AnswerCallbackQuery(ctx, q.ID, b.t(r, "bot.stale", nil), false)
		return b.showMenu(ctx, r)
	}
	if err := act(ctx, r, d); err != nil {
		return b.fail(ctx, r, err)
	}
	return b.answer(ctx, r, "")
}

// onPrivate это сообщение абонента в личке: команда, ожидаемый ввод или текст в поддержку.
func (b *Bot) onPrivate(ctx context.Context, m *tg.Message) error {
	if m.From == nil || m.From.IsBot {
		return nil
	}
	if m.SuccessfulPayment != nil || m.RefundedPayment != nil {
		return b.onPayment(ctx, m)
	}
	r, ok, err := b.enter(ctx, *m.From, m.Chat.ID, "", refFromStart(m.Text))
	if err != nil || !ok {
		return err
	}
	if cmd, arg, isCmd := command(m.Text); isCmd {
		b.waits.clear(m.From.ID)
		err = b.onCommand(ctx, r, cmd, arg)
	} else if w, waiting := b.waits.take(m.From.ID, b.now()); waiting {
		err = b.onInput(ctx, r, w, strings.TrimSpace(m.Text))
	} else {
		err = b.toSupport(ctx, r, m)
	}
	if err != nil {
		return b.fail(ctx, r, err)
	}
	return nil
}

// enter пускает абонента: флуд, регистрация, бан, техработы. ok=false значит дальше не идём.
// ref это код реферальной ссылки: он учитывается, только если абонент новый.
func (b *Bot) enter(ctx context.Context, from tg.User, chatID int64, cbID, ref string) (*req, bool, error) {
	r := &req{from: from, chatID: chatID, cbID: cbID, lang: from.LanguageCode}
	ok, warn := b.flood.allow(from.ID, b.now())
	if !ok {
		if warn {
			return r, false, b.answer(ctx, r, b.t(r, "bot.too_fast", nil))
		}
		return r, false, nil // лишние нажатия не тратят общий лимит отправки в Telegram
	}
	u, err := b.core.UserByTg(ctx, from.ID)
	if errors.Is(err, core.ErrNotFound) {
		u, err = b.core.RegisterRef(ctx, from.ID, from.Username, b.lang(from.LanguageCode), ref)
	}
	if err != nil {
		return r, false, err
	}
	r.user, r.lang = u, u.Lang
	if !u.UnreachableAt.IsZero() {
		if err := b.core.MarkReachable(ctx, u.ID); err != nil {
			return r, false, err
		}
	}
	if !u.BannedAt.IsZero() {
		return r, false, b.banNotice(ctx, r)
	}
	if err := b.core.Serving(ctx, u.ID); errors.Is(err, core.ErrMaintenance) {
		_ = b.answer(ctx, r, "")
		return r, false, b.say(ctx, chatID, b.t(r, "bot.maintenance", nil))
	} else if err != nil {
		return r, false, err
	}
	return r, true, nil
}

// banNotice показывает забаненному текст о блокировке не чаще раза в час. Ядро при этом не зовётся.
func (b *Bot) banNotice(ctx context.Context, r *req) error {
	_ = b.answer(ctx, r, "")
	b.mu.Lock()
	last, seen := b.noticed[r.from.ID]
	show := !seen || b.now().Sub(last) >= banNoticeEvery
	if show {
		b.noticed[r.from.ID] = b.now()
	}
	b.mu.Unlock()
	if !show {
		return nil
	}
	return b.say(ctx, r.chatID, b.t(r, "bot.banned", nil))
}

// answer отвечает на нажатие кнопки, если оно было. Без ответа Telegram крутит часики на кнопке.
func (b *Bot) answer(ctx context.Context, r *req, text string) error {
	if r.cbID == "" {
		return nil
	}
	id := r.cbID
	r.cbID = ""
	return b.api.AnswerCallbackQuery(ctx, id, text, false)
}

// fail показывает абоненту ошибку ядра текстом с кодом. Неизвестная ошибка: общий текст, подробности в журнал.
func (b *Bot) fail(ctx context.Context, r *req, err error) error {
	var ce *core.Error
	text := b.t(r, "bot.error", nil)
	if errors.As(err, &ce) {
		text = b.t(r, ce.Key, nil) + " (" + ce.Code + ")"
	} else {
		b.log.Warn("ошибка в обработчике", "user", r.user.ID, "err", err)
	}
	if r.cbID != "" {
		return b.api.AnswerCallbackQuery(ctx, r.cbID, text, true)
	}
	return b.say(ctx, r.chatID, text)
}

// command разбирает `/cmd arg` и `/cmd@bot arg`.
func command(text string) (cmd, arg string, ok bool) {
	if !strings.HasPrefix(text, "/") {
		return "", "", false
	}
	cmd, arg, _ = strings.Cut(text[1:], " ")
	cmd, _, _ = strings.Cut(cmd, "@")
	return strings.ToLower(cmd), strings.TrimSpace(arg), true
}
