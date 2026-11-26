package bot

import (
	"context"
	"errors"
	"time"

	"github.com/bini69-oi/worker/core"
	"github.com/bini69-oi/worker/tg"
)

// joinWait это сколько бот ждёт код привязки в группе, куда его добавили. Потом выходит.
const joinWait = 10 * time.Minute

// onGroup разбирает сообщения в группах: привязка группы кодом и ответы поддержки.
func (b *Bot) onGroup(ctx context.Context, m *tg.Message) error {
	if m.From == nil || m.From.IsBot {
		return nil
	}
	if cmd, arg, ok := command(m.Text); ok && cmd == "bind" {
		if m.From.ID != b.owner {
			return nil // привязывает только владелец, остальным не отвечаем
		}
		return b.bindChat(ctx, m, arg)
	}
	ch, err := b.core.AdminChat(ctx)
	if err != nil && !errors.Is(err, core.ErrNotFound) {
		return err
	}
	if ch.ChatID == m.Chat.ID {
		return b.fromSupportChat(ctx, m)
	}
	// Чужая группа. Только что добавили: ждём код. Иначе выходим, в том числе после перезапуска,
	// когда бот уже не помнит, что его добавляли.
	b.mu.Lock()
	at, waiting := b.joined[m.Chat.ID]
	b.mu.Unlock()
	if waiting && b.now().Sub(at) < joinWait {
		return nil
	}
	return b.api.LeaveChat(ctx, m.Chat.ID)
}

// bindChat привязывает группу кодом из админки и создаёт темы. Темы нужны, поэтому сначала проверяем форум:
// код одноразовый и не должен сгореть на группе без тем.
func (b *Bot) bindChat(ctx context.Context, m *tg.Message, code string) error {
	reply := func(key string) error { return b.replyIn(ctx, m, key) }
	if !m.Chat.IsForum {
		return reply("chat.need_forum")
	}
	if err := b.core.BindChat(ctx, m.Chat.ID, m.From.ID, code); err != nil {
		if errors.Is(err, core.ErrForbidden) {
			return reply("chat.bad_code")
		}
		return err
	}
	b.mu.Lock()
	delete(b.joined, m.Chat.ID)
	b.mu.Unlock()
	ch := core.AdminChat{ChatID: m.Chat.ID}
	for _, topic := range core.Topics {
		if _, err := b.topic(ctx, ch, topic, true); err != nil {
			return err
		}
		if cur, err := b.core.AdminChat(ctx); err == nil {
			ch = cur
		}
	}
	return reply("chat.bound")
}

// onMember запоминает группу, куда добавили бота. Не привязали за 10 минут: бот выходит.
func (b *Bot) onMember(ctx context.Context, u *tg.ChatMemberUpdated) error {
	if u.Chat.Type != "group" && u.Chat.Type != "supergroup" {
		return nil
	}
	added := (u.NewChatMember.Status == "member" || u.NewChatMember.Status == "administrator") &&
		(u.OldChatMember.Status == "left" || u.OldChatMember.Status == "kicked" || u.OldChatMember.Status == "")
	if !added {
		return nil
	}
	b.mu.Lock()
	b.joined[u.Chat.ID] = b.now()
	b.mu.Unlock()
	return nil
}

// sweepGroups выходит из групп, которые так и не привязали. Привязанную группу не трогает.
func (b *Bot) sweepGroups(ctx context.Context) {
	bound := int64(0)
	if ch, err := b.core.AdminChat(ctx); err == nil {
		bound = ch.ChatID
	}
	b.mu.Lock()
	var leave []int64
	for id, at := range b.joined {
		if id == bound {
			delete(b.joined, id)
		} else if b.now().Sub(at) >= joinWait {
			leave = append(leave, id)
			delete(b.joined, id)
		}
	}
	b.mu.Unlock()
	for _, id := range leave {
		if err := b.api.LeaveChat(ctx, id); err != nil {
			// Не вышли: следующее сообщение в этой группе снова попросит выйти (onGroup).
			b.log.Warn("не вышел из чужой группы", "chat", id, "err", err)
		}
	}
}

// adminAct говорит, что кнопка админская: подтверждения, коды входа, действия под событиями.
func adminAct(act string) bool { return act == "cf" || act == "lg" || act == "adm" }

// onAdminCallback разбирает нажатия админов. Права проверяет ядро по тому, кто нажал.
func (b *Bot) onAdminCallback(ctx context.Context, q *tg.CallbackQuery, d Data) error {
	r := &req{lang: b.lang(""), cbID: q.ID}
	if ok, _ := b.flood.allow(q.From.ID, b.now()); !ok {
		return nil // лишние нажатия не тратят даже ответ Telegram
	}
	id, ok := d.Int(0)
	if q.Message == nil || len(d.Args) < 2 || (!ok && d.Act != "adm") {
		return b.answer(ctx, r, "")
	}
	switch d.Act {
	case "cf":
		return b.onConfirm(ctx, r, q, id, d.Args[1])
	case "lg":
		return b.onLoginCode(ctx, r, q, id, d.Args[1])
	}
	return b.onUserAction(ctx, r, q, d)
}

func (b *Bot) onConfirm(ctx context.Context, r *req, q *tg.CallbackQuery, id int64, ans string) error {
	if ans != "y" && ans != "n" {
		return b.answer(ctx, r, "")
	}
	err := b.core.AnswerConfirm(ctx, id, q.From.ID, ans == "y")
	result := "confirm.done"
	if ans == "n" {
		result = "confirm.rejected"
	}
	return b.afterConfirm(ctx, r, q, id, result, err)
}

func (b *Bot) onLoginCode(ctx context.Context, r *req, q *tg.CallbackQuery, id int64, code string) error {
	if code == "d" {
		return b.answer(ctx, r, "")
	}
	ok, err := b.core.AnswerLoginCode(ctx, id, q.From.ID, code)
	result := "confirm.login_no"
	if ok {
		result = "confirm.login_ok"
	}
	return b.afterConfirm(ctx, r, q, id, result, err)
}

// afterConfirm заменяет кнопки итогом. Нажал чужой: всплывающее «нет прав», подтверждение не меняется.
func (b *Bot) afterConfirm(ctx context.Context, r *req, q *tg.CallbackQuery, id int64, result string, err error) error {
	switch {
	case errors.Is(err, core.ErrForbidden):
		return b.api.AnswerCallbackQuery(ctx, q.ID, b.t(r, "adm.no_rights", nil), true)
	case errors.Is(err, core.ErrConfirmGone):
		result = "confirm.gone"
	case err != nil:
		_ = b.answer(ctx, r, b.t(r, "bot.error", nil))
		return err
	}
	done := Screen{Rows: [][]Button{{b.btn(r, result, data("lg", id, "d"))}}}
	if err := b.api.EditMessageReplyMarkup(ctx, q.Message.Chat.ID, q.Message.MessageID, done.markup()); err != nil {
		return err
	}
	return b.answer(ctx, r, b.t(r, result, nil))
}

// topicOf это буква темы в данных кнопки: по ней кнопки под событием собираются заново.
var topicOf = map[string]string{"p": core.TopicPayments, "t": core.TopicTrials, "n": core.TopicNew}

// onUserAction это «Бан», «Разбанить» и «+7 дней» под событием в группе.
func (b *Bot) onUserAction(ctx context.Context, r *req, q *tg.CallbackQuery, d Data) error {
	uid, ok := d.Int(1)
	if !ok || len(d.Args) < 3 {
		return b.answer(ctx, r, "")
	}
	// Посторонний в группе не доходит до ядра: иначе каждое его нажатие писало бы в журнал и слало тревогу.
	if !b.core.IsStaff(ctx, q.From.ID) {
		return b.api.AnswerCallbackQuery(ctx, q.ID, b.t(r, "adm.no_rights", nil), true)
	}
	// «+7 дней» под одним событием срабатывает один раз, кто бы и сколько ни нажал.
	key := pressKey(q.Message.Chat.ID, q.Message.MessageID, d)
	if d.Args[0] == "days" && !b.onceKey(key) {
		return b.answer(ctx, r, b.t(r, "bot.busy", nil))
	}
	topic := topicOf[d.Args[2]]
	var err error
	banned := false
	switch d.Args[0] {
	case "ban":
		err, banned = b.core.Ban(ctx, q.From.ID, uid, ""), true
	case "unban":
		err = b.core.Unban(ctx, q.From.ID, uid, "")
	case "days":
		topic = core.TopicPayments
		_, err = b.core.GrantDays(ctx, q.From.ID, uid, 7)
	default:
		return b.answer(ctx, r, "")
	}
	if err != nil && d.Args[0] == "days" {
		b.forget(key)
	}
	if errors.Is(err, core.ErrForbidden) {
		return b.api.AnswerCallbackQuery(ctx, q.ID, b.t(r, "adm.no_rights", nil), true)
	}
	if err != nil {
		return b.fail(ctx, r, err)
	}
	if d.Args[0] == "days" {
		return b.answer(ctx, r, b.t(r, "adm.done", nil))
	}
	rows := Screen{Rows: b.adminRows(r, topic, uid, banned)}
	if err := b.api.EditMessageReplyMarkup(ctx, q.Message.Chat.ID, q.Message.MessageID, rows.markup()); err != nil {
		return err
	}
	return b.answer(ctx, r, b.t(r, "adm.done", nil))
}

// replyIn отвечает в группе реплаем на сообщение, в той же теме.
func (b *Bot) replyIn(ctx context.Context, m *tg.Message, key string) error {
	_, err := b.api.SendMessage(ctx, tg.SendParams{ChatID: m.Chat.ID, ThreadID: m.ThreadID, Text: b.t(b.sysReq(), key, nil), ReplyTo: m.MessageID})
	return err
}
