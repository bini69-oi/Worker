package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"strconv"
	"sync/atomic"

	"github.com/bini69-oi/worker/bot"
	"github.com/bini69-oi/worker/core"
	"github.com/bini69-oi/worker/tg"
)

// telegram это всё, что связывает процесс с Telegram: клиент, бот, канал обновлений и секрет вебхука.
type telegram struct {
	client  *tg.Client
	bot     *bot.Bot
	updates chan tg.Update
	secret  string
	name    atomic.Value // имя бота без @, узнаём у Telegram при старте
	polling func() bool
	hook    string
	log     *slog.Logger
}

func newTelegram(token, apiURL, publicURL string, polling func() bool, log *slog.Logger) *telegram {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	t := &telegram{client: &tg.Client{Token: token, BaseURL: apiURL}, updates: make(chan tg.Update, 256), secret: hex.EncodeToString(b),
		polling: polling, hook: publicURL + "/tg/webhook", log: log}
	t.name.Store("")
	return t
}

// botName отдаёт имя бота. Пока Telegram не ответил, имя пустое.
func (t *telegram) botName() string { return t.name.Load().(string) }

// paidURL это адрес возврата из кассы в бота: диплинк paid_<номер платежа>.
func (t *telegram) paidURL(paymentID int64) string {
	if t.botName() == "" {
		return ""
	}
	return "https://t.me/" + t.botName() + "?start=paid_" + strconv.FormatInt(paymentID, 10)
}

// start узнаёт имя бота, ставит вебхук (или снимает его для опроса), команды и запускает разбор обновлений.
// Telegram недоступен при старте: процесс работает дальше, кассы и панель не ждут Telegram.
func (t *telegram) start(ctx context.Context, run func(func())) {
	if me, err := t.client.GetMe(ctx); err != nil {
		t.log.Warn("Telegram не ответил на getMe", "err", err)
	} else {
		t.name.Store(me.Username)
		t.bot.SetName(me.Username)
	}
	if t.polling() {
		if err := t.client.DeleteWebhook(ctx); err != nil {
			t.log.Warn("не снят вебхук для опроса", "err", err)
		}
		run(func() { _ = tg.Poll(ctx, t.client, t.updates) })
	} else if err := t.client.SetWebhook(ctx, t.hook, t.secret, bot.Allowed); err != nil {
		t.log.Warn("вебхук не поставлен", "err", err)
	}
	if err := t.bot.Setup(ctx); err != nil {
		t.log.Warn("команды бота не поставлены", "err", err)
	}
	run(func() { t.bot.Run(ctx, t.updates) })
}

// notifier отдаёт уведомления ядра боту, который собирается после ядра.
type notifier struct{ b *bot.Bot }

func (n *notifier) User(ctx context.Context, x core.NotifyTask) error     { return n.b.User(ctx, x) }
func (n *notifier) Admin(ctx context.Context, x core.AdminTask) error     { return n.b.Admin(ctx, x) }
func (n *notifier) Confirm(ctx context.Context, x core.ConfirmTask) error { return n.b.Confirm(ctx, x) }
