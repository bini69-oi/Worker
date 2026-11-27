package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"strconv"
	"sync/atomic"
	"time"

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
	// publicURL и source нужны, чтобы вернуть из кассы в кабинет, если платёж создан в браузере.
	publicURL string
	source    func(paymentID int64) string
	log       *slog.Logger
	// retryWait это первая пауза между повторами шагов связи с Telegram.
	retryWait time.Duration
}

func newTelegram(token, apiURL, publicURL string, polling func() bool, log *slog.Logger) *telegram {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	t := &telegram{client: &tg.Client{Token: token, BaseURL: apiURL}, updates: make(chan tg.Update, 256), secret: hex.EncodeToString(b),
		polling: polling, hook: publicURL + "/tg/webhook", publicURL: publicURL, log: log, retryWait: 5 * time.Second}
	t.name.Store("")
	return t
}

// botName отдаёт имя бота. Пока Telegram не ответил, имя пустое.
func (t *telegram) botName() string { return t.name.Load().(string) }

// paidURL это адрес возврата из кассы в бота: диплинк paid_<номер платежа>.
func (t *telegram) paidURL(paymentID int64) string {
	// Платёж из кабинета в браузере возвращается на страницу кабинета, остальные в бота.
	if t.source != nil && t.publicURL != "" && t.source(paymentID) == core.PaySourceWeb {
		return t.publicURL + "/app/#/pay/" + strconv.FormatInt(paymentID, 10)
	}
	if t.botName() == "" {
		return ""
	}
	return "https://t.me/" + t.botName() + "?start=paid_" + strconv.FormatInt(paymentID, 10)
}

// start запускает разбор обновлений сразу, а связь с Telegram (имя бота, вебхук или опрос, команды)
// настраивает в фоне с повторами: Telegram недоступен при старте, кассы и панель его не ждут.
func (t *telegram) start(ctx context.Context, run func(func())) {
	run(func() { t.bot.Run(ctx, t.updates) })
	run(func() {
		t.retry(ctx, "getMe", func() error {
			me, err := t.client.GetMe(ctx)
			if err == nil {
				t.name.Store(me.Username)
				t.bot.SetName(me.Username)
			}
			return err
		})
		if t.polling() {
			t.retry(ctx, "deleteWebhook", func() error { return t.client.DeleteWebhook(ctx) })
			run(func() { _ = tg.Poll(ctx, t.client, t.updates) })
		} else {
			t.retry(ctx, "setWebhook", func() error { return t.client.SetWebhook(ctx, t.hook, t.secret, bot.Allowed) })
		}
		t.retry(ctx, "setMyCommands", func() error { return t.bot.Setup(ctx) })
	})
}

// retry повторяет шаг с паузой от retryWait до минуты, пока он не пройдёт или процесс не остановят.
func (t *telegram) retry(ctx context.Context, step string, fn func() error) {
	wait := t.retryWait
	for {
		err := fn()
		if err == nil || ctx.Err() != nil {
			return
		}
		t.log.Warn("Telegram: шаг не прошёл, повтор", "step", step, "wait", wait.String(), "err", err)
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		wait = min(wait*2, time.Minute)
	}
}

// notifier отдаёт уведомления ядра боту, который собирается после ядра.
type notifier struct{ b *bot.Bot }

func (n *notifier) User(ctx context.Context, x core.NotifyTask) error     { return n.b.User(ctx, x) }
func (n *notifier) Admin(ctx context.Context, x core.AdminTask) error     { return n.b.Admin(ctx, x) }
func (n *notifier) Confirm(ctx context.Context, x core.ConfirmTask) error { return n.b.Confirm(ctx, x) }
func (n *notifier) Owner(ctx context.Context, x core.AdminTask) error     { return n.b.Owner(ctx, x) }
