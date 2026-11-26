// Package bot это текстовый Telegram-бот абонента и уведомления админам. Логики продажи здесь нет:
// экран берёт данные у ядра и зовёт его сценарии, а ядро проверяет права, деньги и владельца объекта.
package bot

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/bini69-oi/worker/core"
	"github.com/bini69-oi/worker/texts"
	"github.com/bini69-oi/worker/tg"
)

// Options это всё, что нужно боту.
type Options struct {
	Core    *core.Core
	Texts   *texts.Texts
	API     API
	Config  core.Config
	Log     *slog.Logger
	Now     func() time.Time
	Workers int    // по умолчанию 8
	Name    string // имя бота без @, для ссылок t.me
	URL     string // публичный адрес Worker, для переходов /dl и Mini App
	Owner   int64  // Telegram ID владельца
	DLKey   []byte // ключ подписи переходов /dl
}

// Bot разбирает обновления Telegram.
type Bot struct {
	core    *core.Core
	tx      *texts.Texts
	api     API
	cfg     core.Config
	log     *slog.Logger
	now     func() time.Time
	workers int
	name    string
	url     string
	owner   int64
	dlKey   []byte

	flood  *flood
	waits  *waits
	acts   map[string]action
	inputs map[waitKind]inputFn
	dedup  *tg.Dedup

	mu      sync.Mutex
	noticed map[int64]time.Time  // кому и когда показан текст о бане
	pressed map[string]time.Time // нажатые кнопки оплаты, чтобы второй клик не платил
}

// action это обработчик нажатия кнопки абонента.
type action func(ctx context.Context, r *req, d Data) error

// req это одно действие абонента: кто, где и в каком сообщении.
type req struct {
	user   core.User
	from   tg.User
	chatID int64
	msgID  int    // сообщение с нажатой кнопкой; 0 для команд и текста
	cbID   string // id нажатия, на него надо ответить
	lang   string
}

// New собирает бота.
func New(o Options) *Bot {
	if o.Workers <= 0 {
		o.Workers = 8
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Log == nil {
		o.Log = slog.Default()
	}
	b := &Bot{core: o.Core, tx: o.Texts, api: o.API, cfg: o.Config, log: o.Log, now: o.Now,
		workers: o.Workers, name: o.Name, url: o.URL, owner: o.Owner, dlKey: o.DLKey,
		flood: newFlood(5, 5), waits: newWaits(), acts: map[string]action{}, inputs: map[waitKind]inputFn{},
		dedup: tg.NewDedup(tg.DedupSize), noticed: map[int64]time.Time{}, pressed: map[string]time.Time{}}
	b.register()
	return b
}

// Run разбирает обновления, пока не закончится ctx. Обновления одного человека идут строго по очереди:
// воркер выбирается по его Telegram ID. Так двойной клик не обработают два воркера одновременно.
func (b *Bot) Run(ctx context.Context, in <-chan tg.Update) {
	queues := make([]chan tg.Update, b.workers)
	var wg sync.WaitGroup
	for i := range queues {
		queues[i] = make(chan tg.Update, 64)
		wg.Add(1)
		go func(q <-chan tg.Update) {
			defer wg.Done()
			for u := range q {
				b.safeHandle(ctx, u)
			}
		}(queues[i])
	}
	defer func() {
		for _, q := range queues {
			close(q)
		}
		wg.Wait()
	}()
	for {
		select {
		case <-ctx.Done():
			return
		case u := <-in:
			if b.dedup.Seen(u.UpdateID) {
				continue
			}
			id := senderID(u)
			if id < 0 {
				id = -id
			}
			select {
			case queues[id%int64(b.workers)] <- u:
			case <-ctx.Done():
				return
			}
		}
	}
}

// senderID это тот, чьи обновления идут по очереди. Для группы без автора это сам чат.
func senderID(u tg.Update) int64 {
	switch {
	case u.CallbackQuery != nil:
		return u.CallbackQuery.From.ID
	case u.PreCheckoutQuery != nil:
		return u.PreCheckoutQuery.From.ID
	case u.Message != nil && u.Message.From != nil:
		return u.Message.From.ID
	case u.Message != nil:
		return u.Message.Chat.ID
	case u.MyChatMember != nil:
		return u.MyChatMember.Chat.ID
	}
	return 0
}

// safeHandle не даёт одной ошибке в обработчике уронить воркер.
func (b *Bot) safeHandle(ctx context.Context, u tg.Update) {
	defer func() {
		if p := recover(); p != nil {
			b.log.Error("паника в обработчике обновления", "update", u.UpdateID, "panic", p)
		}
	}()
	hctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if err := b.handle(hctx, u); err != nil && !errors.Is(err, context.Canceled) {
		b.log.Warn("обновление не обработано", "update", u.UpdateID, "err", err)
	}
}
