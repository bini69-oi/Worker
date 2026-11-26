package bot

import (
	"context"
	"errors"
	"maps"
	"slices"
	"strings"
	"sync"

	"github.com/bini69-oi/worker/core"
	"github.com/bini69-oi/worker/tg"
)

// topicMu не даёт двум уведомлениям одновременно создать одну и ту же тему.
var topicMu sync.Mutex

// Admin шлёт событие в тему группы по его виду. Тема выключена или группы нет: молчим.
// Тревоги безопасности владельцу в личку идут отдельной задачей (Owner), тема «Безопасность» не выключается.
func (b *Bot) Admin(ctx context.Context, n core.AdminTask) error {
	if n.Topic != core.TopicSecurity && !b.cfg.Bool("notify."+n.Topic) {
		return nil
	}
	ch, err := b.core.AdminChat(ctx)
	if errors.Is(err, core.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	r := b.sysReq()
	text, uid := b.eventText(ctx, r, n)
	return b.toTopic(ctx, ch, n.Topic, Screen{Text: text, Rows: b.adminRows(r, n.Topic, uid, false)})
}

// Owner шлёт тревогу безопасности владельцу в личку.
func (b *Bot) Owner(ctx context.Context, n core.AdminTask) error {
	text, _ := b.eventText(ctx, b.sysReq(), n)
	_, err := b.api.SendMessage(ctx, tg.SendParams{ChatID: b.owner, Text: text, ParseMode: "HTML", DisablePreview: true})
	return err
}

// eventText собирает текст события: копейки рублями, абонент ссылкой. Отдаёт и id абонента для кнопок.
func (b *Bot) eventText(ctx context.Context, r *req, n core.AdminTask) (string, int64) {
	vars := moneyFmt(n.Vars)
	var uid int64
	if id, ok := parseInt(n.Vars["user_id"]); ok && id > 0 {
		if u, err := b.core.User(ctx, id); err == nil {
			uid, vars["user"] = id, mention(u)
		}
	}
	return b.t(r, n.Key, vars), uid
}

// toTopic шлёт сообщение в тему.
func (b *Bot) toTopic(ctx context.Context, ch core.AdminChat, topic string, s Screen) error {
	return b.inTopic(ctx, ch, topic, func(thread int) error {
		_, err := b.api.SendMessage(ctx, tg.SendParams{ChatID: ch.ChatID, ThreadID: thread, Text: s.Text, ParseMode: "HTML",
			Markup: s.markup(), DisablePreview: true})
		return err
	})
}

// inTopic выполняет send в теме. Тему удалили в группе: создаёт её заново и повторяет один раз.
func (b *Bot) inTopic(ctx context.Context, ch core.AdminChat, topic string, send func(thread int) error) error {
	thread, err := b.topic(ctx, ch, topic, false)
	if err != nil {
		return err
	}
	err = send(thread)
	var te *tg.Error
	if errors.As(err, &te) && te.IsNotFound() {
		if thread, err = b.topic(ctx, ch, topic, true); err != nil {
			return err
		}
		return send(thread)
	}
	return err
}

// topic отдаёт номер темы, при нужде создаёт её и запоминает в ядре.
func (b *Bot) topic(ctx context.Context, ch core.AdminChat, topic string, fresh bool) (int, error) {
	topicMu.Lock()
	defer topicMu.Unlock()
	// Чат перечитываем под замком: другой обработчик мог только что создать соседнюю тему.
	if cur, err := b.core.AdminChat(ctx); err == nil {
		ch = cur
	}
	if id, ok := ch.Topics[topic]; ok && !fresh {
		return int(id), nil
	}
	id, err := b.api.CreateForumTopic(ctx, ch.ChatID, b.t(b.sysReq(), "topic."+topic, nil))
	if err != nil {
		return 0, err
	}
	topics := maps.Clone(ch.Topics)
	if topics == nil {
		topics = map[string]int64{}
	}
	topics[topic] = int64(id)
	return id, b.core.SetChatTopics(ctx, topics)
}

// adminRows это кнопки под событием: бан, +7 дней, открыть в админке. Только для событий об абоненте.
func (b *Bot) adminRows(r *req, topic string, uid int64, banned bool) [][]Button {
	if uid == 0 || (topic != core.TopicPayments && topic != core.TopicTrials && topic != core.TopicNew) {
		return nil
	}
	letter := topic[:1] // p, t, n: тема в данных кнопки, чтобы собрать кнопки заново после нажатия
	ban := b.btn(r, "btn.ban", data("adm", "ban", uid, letter))
	if banned {
		ban = b.btn(r, "btn.unban", data("adm", "unban", uid, letter))
	}
	row := []Button{ban}
	if topic == core.TopicPayments {
		row = append(row, b.btn(r, "btn.days7", data("adm", "days", uid, letter)))
	}
	rows := [][]Button{row}
	if b.url != "" {
		rows = append(rows, []Button{{Text: b.t(r, "btn.open_admin", nil), URL: b.url + "/admin/#/users/" + itoa(uid)}})
	}
	return rows
}

// Confirm шлёт админу в личку подтверждение. Вход: три кода и «Это не я». Остальное: «Да» и «Нет».
func (b *Bot) Confirm(ctx context.Context, n core.ConfirmTask) error {
	r := &req{lang: b.lang("")}
	id := n.ConfirmID
	var s Screen
	if n.Kind == "login" {
		s.Text = b.t(r, "confirm.login", n.Vars)
		var row []Button
		for _, code := range strings.Split(n.Vars["codes"], ",") {
			if word(code, 1, 8, false) {
				row = append(row, Button{Text: code, Data: data("lg", id, code)})
			}
		}
		s.Rows = [][]Button{row, {b.btn(r, "btn.not_me", data("lg", id, "no"))}}
	} else {
		vars := map[string]string{"kind": n.Kind, "by": n.Vars["by"], "ip": n.Vars["ip"], "details": confirmDetails(n.Vars)}
		s.Text = b.t(r, "confirm.action", vars)
		s.Rows = [][]Button{{b.btn(r, "btn.yes", data("cf", id, "y")), b.btn(r, "btn.no", data("cf", id, "n"))}}
	}
	_, err := b.api.SendMessage(ctx, tg.SendParams{ChatID: n.AdminTgID, Text: s.Text, ParseMode: "HTML", Markup: s.markup(), DisablePreview: true})
	return err
}

// confirmDetails пишет остальные подстановки подтверждения строками «ключ: значение».
func confirmDetails(vars map[string]string) string {
	var lines []string
	for _, k := range slices.Sorted(maps.Keys(vars)) {
		if k != "by" && k != "ip" && k != "codes" {
			lines = append(lines, k+": "+vars[k])
		}
	}
	return strings.Join(lines, "\n")
}
