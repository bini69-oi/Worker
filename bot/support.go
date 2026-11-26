package bot

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/bini69-oi/worker/core"
	"github.com/bini69-oi/worker/tg"
)

const (
	cardEvery      = time.Hour // карточка абонента в теме не чаще раза в час
	supportPerHour = 30        // сообщений в поддержку от абонента за час
)

// supportState это память поддержки: когда была карточка и сколько сообщений за час.
type supportState struct {
	mu    sync.Mutex
	card  map[int64]time.Time
	count map[int64]*hourCount
	sweep time.Time
}

type hourCount struct {
	start  time.Time
	n      int
	warned bool
}

func newSupportState() *supportState {
	return &supportState{card: map[int64]time.Time{}, count: map[int64]*hourCount{}}
}

// take засчитывает сообщение. ok: пересылать. warn: один раз сказать, что сообщений слишком много.
func (s *supportState) take(id int64, now time.Time) (ok, warn bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if now.Sub(s.sweep) > time.Hour { // старые счётчики и отметки больше не нужны: память не растёт
		for k, c := range s.count {
			if now.Sub(c.start) >= time.Hour {
				delete(s.count, k)
			}
		}
		for k, at := range s.card {
			if now.Sub(at) >= cardEvery {
				delete(s.card, k)
			}
		}
		s.sweep = now
	}
	c := s.count[id]
	if c == nil || now.Sub(c.start) >= time.Hour {
		c = &hourCount{start: now}
		s.count[id] = c
	}
	c.n++
	if c.n <= supportPerHour {
		return true, false
	}
	if c.warned {
		return false, false
	}
	c.warned = true
	return false, true
}

// needCard говорит, слать ли карточку перед сообщением. Отметку ставит cardSent после отправки:
// если карточка не ушла, следующее сообщение придёт с ней.
func (s *supportState) needCard(id int64, now time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	last, ok := s.card[id]
	return !ok || now.Sub(last) >= cardEvery
}

func (s *supportState) cardSent(id int64, now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.card[id] = now
}

// toSupport копирует сообщение абонента в тему «Поддержка» с карточкой раз в час.
// Группы нет или тема выключена: абонент видит контакт поддержки из настроек.
func (b *Bot) toSupport(ctx context.Context, r *req, m *tg.Message) error {
	ch, err := b.core.AdminChat(ctx)
	if errors.Is(err, core.ErrNotFound) || (err == nil && !b.cfg.Bool("notify.support")) {
		return b.say(ctx, r.chatID, b.t(r, "support.contact", map[string]string{"contact": b.cfg.String("support.contact")}))
	}
	if err != nil {
		return err
	}
	ok, warn := b.support.take(r.from.ID, b.now())
	if !ok {
		if warn {
			return b.say(ctx, r.chatID, b.t(r, "support.limit", nil))
		}
		return nil
	}
	card := b.support.needCard(r.from.ID, b.now())
	err = b.inTopic(ctx, ch, core.TopicSupport, func(thread int) error {
		if card {
			if err := b.supportCard(ctx, ch.ChatID, thread, r.user.ID); err != nil {
				return err
			}
			b.support.cardSent(r.from.ID, b.now())
			card = false
		}
		id, err := b.api.CopyMessage(ctx, ch.ChatID, thread, m.Chat.ID, m.MessageID, 0)
		if err != nil {
			return err
		}
		return b.core.LinkSupport(ctx, ch.ChatID, int64(id), r.user.ID)
	})
	if err != nil {
		return err
	}
	return b.say(ctx, r.chatID, b.t(r, "support.sent", nil))
}

// supportCard шлёт в тему карточку абонента: кто, тариф, срок, баланс. Реплай на неё тоже уходит абоненту.
func (b *Bot) supportCard(ctx context.Context, chat int64, thread int, userID int64) error {
	card, err := b.core.SupportCard(ctx, userID)
	if err != nil {
		return err
	}
	r := b.sysReq()
	tariff, until := b.t(r, "status.none", nil), "-"
	if !card.ExpiresAt.IsZero() {
		tariff, until = card.Tariff, date(card.ExpiresAt)
		if card.Trial {
			tariff = b.t(r, "sub.trial_name", nil)
		}
	}
	text := b.t(r, "support.card", map[string]string{"user": mention(card.User), "tg_id": itoa(card.User.TgID),
		"tariff": tariff, "until": until, "balance": money(card.User.Balance)})
	m, err := b.api.SendMessage(ctx, tg.SendParams{ChatID: chat, ThreadID: thread, Text: text, ParseMode: "HTML", DisablePreview: true})
	if err != nil {
		return err
	}
	return b.core.LinkSupport(ctx, chat, int64(m.MessageID), userID)
}

// fromSupportChat разбирает сообщения в привязанной группе: реплай админа с правом support
// на сообщение абонента копируется абоненту. Сообщение без реплая остаётся внутри группы.
func (b *Bot) fromSupportChat(ctx context.Context, m *tg.Message) error {
	// В группе с темами обычное сообщение в теме приходит «реплаем» на первое сообщение темы.
	// Это не ответ абоненту.
	if m.ReplyToMessage == nil || m.ReplyToMessage.MessageID == m.ThreadID {
		return nil
	}
	ch, err := b.core.AdminChat(ctx)
	if err != nil || !b.core.IsAdmin(ctx, m.From.ID, core.PermSupport) {
		return err
	}
	uid, err := b.core.SupportUser(ctx, m.Chat.ID, int64(m.ReplyToMessage.MessageID))
	if errors.Is(err, core.ErrNotFound) {
		if m.ThreadID != 0 && int64(m.ThreadID) == ch.Topics[core.TopicSupport] {
			return b.replyIn(ctx, m, "support.too_old")
		}
		return nil // реплай на событие или чужое сообщение: не поддержка
	}
	if err != nil {
		return err
	}
	u, err := b.core.User(ctx, uid)
	if err != nil {
		return err
	}
	_, err = b.api.CopyMessage(ctx, u.TgID, 0, m.Chat.ID, m.MessageID, 0)
	var te *tg.Error
	if errors.As(err, &te) && te.IsBlocked() {
		if err := b.core.MarkUnreachable(ctx, u.ID); err != nil {
			return err
		}
		return b.replyIn(ctx, m, "support.blocked")
	}
	return err
}
