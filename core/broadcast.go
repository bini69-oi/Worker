package core

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

// Статусы рассылки.
const (
	BcDraft     = "draft"
	BcRunning   = "running"
	BcDone      = "done"
	BcCancelled = "cancelled"
)

// Сегменты рассылки (SegExpired30 объявлен рядом с фильтрами абонентов). Забаненные и недоступные (заблокировали бота) не получают её никогда.
const (
	SegAll    = FilterAll    // все
	SegActive = FilterActive // с живой подпиской
	SegNone   = FilterNone   // без подписки
	SegTrial  = FilterTrial  // на пробном
)

// Segments это сегменты, из которых админ выбирает получателей.
var Segments = []string{SegAll, SegActive, SegNone, SegExpired30, SegTrial}

// maxButtonText это предел подписи кнопки Telegram.
const maxButtonText = 64

// Broadcast это рассылка. Получателей отдельной таблицей не храним: порядок по id абонента стабилен,
// хватает курсора CursorUserID.
type Broadcast struct {
	ID           int64
	Segment      string
	Text         string
	ButtonText   string
	ButtonURL    string
	Status       string
	Total        int // получателей на старте
	Sent         int
	Failed       int
	Blocked      int
	CursorUserID int64 // до какого абонента дошли
	CreatedBy    int64
	CreatedAt    time.Time
	StartedAt    time.Time
	DoneAt       time.Time
}

// BroadcastSender отправляет одно сообщение рассылки в личку. Ошибки: ErrRecipientGone (человек заблокировал бота
// или удалил аккаунт), *RetryAfter (Telegram просит подождать), прочее это сбой этого сообщения.
type BroadcastSender interface {
	Send(ctx context.Context, tgID int64, text, buttonText, buttonURL string) error
}

// ErrRecipientGone значит, что сообщение не дойдёт: получатель заблокировал бота.
var ErrRecipientGone = errors.New("получатель недоступен")

// ErrMarkupRejected значит, что Telegram не принял разметку текста: следующим получателям будет то же самое,
// поэтому рассылка останавливается после первого отказа.
var ErrMarkupRejected = errors.New("разметка отвергнута Telegram")

// RetryAfter это ответ 429 Telegram: подождать столько секунд.
type RetryAfter struct{ Seconds int }

func (e *RetryAfter) Error() string {
	return "Telegram просит подождать, повтор через " + itoa(int64(e.Seconds)) + " с"
}

func validSegment(s string) bool { return slices.Contains(Segments, s) }

// CreateBroadcast сохраняет черновик рассылки. Нужно право broadcast. Текст проверяется как разметка Telegram HTML,
// кнопка одна, по желанию, со ссылкой https или tg.
func (c *Core) CreateBroadcast(ctx context.Context, actorTg int64, b Broadcast) (Broadcast, error) {
	b.Text, b.ButtonText, b.ButtonURL = strings.TrimSpace(b.Text), strings.TrimSpace(b.ButtonText), strings.TrimSpace(b.ButtonURL)
	if !validSegment(b.Segment) {
		return b, ErrInvalid
	}
	if err := CheckMarkup(b.Text); err != nil {
		return b, err
	}
	if (b.ButtonText == "") != (b.ButtonURL == "") || utf8.RuneCountInString(b.ButtonText) > maxButtonText {
		return b, ErrInvalid
	}
	if b.ButtonURL != "" {
		if err := CheckButtonURL(b.ButtonURL); err != nil {
			return b, err
		}
	}
	err := c.asAdmin(ctx, actorTg, PermBroadcast, "broadcast.create", func(tx Tx, _ Admin, now time.Time) (string, map[string]string, error) {
		b.Status, b.CreatedBy, b.CreatedAt = BcDraft, actorTg, now
		var err error
		if b.ID, err = tx.CreateBroadcast(b); err != nil {
			return "", nil, err
		}
		return itoa(b.ID), map[string]string{"segment": b.Segment}, nil
	})
	return b, err
}

// CountSegment говорит, сколько абонентов получит рассылка этому сегменту сейчас. Нужно право broadcast.
func (c *Core) CountSegment(ctx context.Context, actorTg int64, segment string) (int, error) {
	if _, err := c.Require(ctx, actorTg, PermBroadcast, "broadcast.count"); err != nil {
		return 0, err
	}
	if !validSegment(segment) {
		return 0, ErrInvalid
	}
	return c.store.SegmentCount(ctx, segment, c.clock.Now())
}

// Broadcasts отдаёт рассылки страницами по PageSize, новые первыми. Нужно право broadcast.
func (c *Core) Broadcasts(ctx context.Context, actorTg, cursor int64) (Page[Broadcast], error) {
	var page Page[Broadcast]
	if _, err := c.Require(ctx, actorTg, PermBroadcast, "broadcast.list"); err != nil {
		return page, err
	}
	list, err := c.store.Broadcasts(ctx, cursor, PageSize+1)
	if err != nil {
		return page, err
	}
	page.Items = list
	if len(list) > PageSize {
		page.Items = list[:PageSize]
		page.Next = page.Items[PageSize-1].ID
	}
	return page, nil
}

// BroadcastByID отдаёт рассылку со счётчиками: страница опрашивает её, пока идёт отправка.
func (c *Core) BroadcastByID(ctx context.Context, actorTg, id int64) (Broadcast, error) {
	if _, err := c.Require(ctx, actorTg, PermBroadcast, "broadcast.get"); err != nil {
		return Broadcast{}, err
	}
	return c.store.BroadcastByID(ctx, id)
}

// TestBroadcast шлёт рассылку только нажавшему админу в личку. Счётчики и статус рассылки не меняются.
func (c *Core) TestBroadcast(ctx context.Context, actorTg, id int64) error {
	if _, err := c.Require(ctx, actorTg, PermBroadcast, "broadcast.test"); err != nil {
		return err
	}
	b, err := c.store.BroadcastByID(ctx, id)
	if err != nil {
		return err
	}
	if c.sender == nil {
		return ErrInternal
	}
	switch err := c.sender.Send(ctx, actorTg, b.Text, b.ButtonText, b.ButtonURL); {
	case err == nil:
		return nil
	case errors.Is(err, ErrRecipientGone):
		return ErrUnreachable // админ не нажимал «Старт» у бота
	default:
		c.log.Warn("тест рассылки не отправился", "err", err)
		return ErrInternal
	}
}

// broadcastStartPayload это то, что владелец видит и подтверждает перед запуском рассылки: кому, сколько и что.
type broadcastStartPayload struct {
	ID      int64  `json:"id"`
	By      int64  `json:"by"`
	Segment string `json:"segment"`
	Total   int    `json:"total"`
	Preview string `json:"preview"`
	Hash    string `json:"hash"`
}

func broadcastHash(b Broadcast) string {
	return hex.EncodeToString(sha(b.Segment + "\x00" + b.Text + "\x00" + b.ButtonText + "\x00" + b.ButtonURL))
}

// StartBroadcast просит запустить черновик. Нужно право broadcast. Рассылка уходит всем абонентам сегмента,
// поэтому запуск ждёт «Да» владельца в Telegram, даже если просит сам владелец (угнанная сессия не разошлёт текст).
// Владелец видит сегмент, число получателей и начало текста. Отдаёт номер подтверждения.
// Пока идёт другая рассылка: ErrBroadcastBusy сразу.
func (c *Core) StartBroadcast(ctx context.Context, actorTg, id int64) (int64, error) {
	if _, err := c.Require(ctx, actorTg, PermBroadcast, "broadcast.start"); err != nil {
		return 0, err
	}
	b, err := c.store.BroadcastByID(ctx, id)
	if err != nil {
		return 0, err
	}
	if b.Status != BcDraft {
		return 0, ErrInvalid
	}
	if cur, err := c.runningBroadcast(ctx); err != nil {
		return 0, err
	} else if cur.ID != 0 {
		return 0, ErrBroadcastBusy
	}
	total, err := c.store.SegmentCount(ctx, b.Segment, c.clock.Now())
	if err != nil {
		return 0, err
	}
	preview := []rune(b.Text)
	return c.RequestAction(ctx, actorTg, "broadcast_start", broadcastStartPayload{ID: id, By: actorTg, Segment: b.Segment,
		Total: total, Preview: string(preview[:min(len(preview), 200)]), Hash: broadcastHash(b)})
}

// onBroadcastApproved запускает рассылку после «Да» владельца. Текст должен остаться тем, что владелец видел.
func (c *Core) onBroadcastApproved(ctx context.Context, payload []byte) error {
	var p broadcastStartPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		return err
	}
	return c.asAdmin(ctx, p.By, PermBroadcast, "broadcast.start", func(tx Tx, _ Admin, now time.Time) (string, map[string]string, error) {
		b, err := tx.Broadcast(p.ID)
		if err != nil {
			return "", nil, err
		}
		if b.Status != BcDraft || broadcastHash(b) != p.Hash {
			return "", nil, ErrInvalid
		}
		if _, err := tx.RunningBroadcast(); err == nil {
			return "", nil, ErrBroadcastBusy
		} else if !errors.Is(err, ErrNotFound) {
			return "", nil, err
		}
		total, err := tx.SegmentCount(b.Segment, now)
		if err != nil {
			return "", nil, err
		}
		if ok, err := tx.StartBroadcast(p.ID, total, now); err != nil || !ok {
			if errors.Is(err, ErrDuplicate) {
				err = ErrBroadcastBusy
			}
			if err == nil {
				err = ErrInvalid
			}
			return "", nil, err
		}
		return itoa(p.ID), map[string]string{"segment": b.Segment, "total": itoa(int64(total))}, nil
	})
}

// CancelBroadcast отменяет черновик или идущую рассылку. Нужно право broadcast. Идущая останавливается
// после текущего сообщения, остальные абоненты ничего не получат.
func (c *Core) CancelBroadcast(ctx context.Context, actorTg, id int64) error {
	return c.asAdmin(ctx, actorTg, PermBroadcast, "broadcast.cancel", func(tx Tx, _ Admin, now time.Time) (string, map[string]string, error) {
		b, err := tx.Broadcast(id)
		if err != nil {
			return "", nil, err
		}
		if ok, err := tx.CancelBroadcast(id, now); err != nil || !ok {
			if err == nil {
				err = ErrInvalid
			}
			return "", nil, err
		}
		return itoa(id), map[string]string{"sent": itoa(int64(b.Sent))}, nil
	})
}
