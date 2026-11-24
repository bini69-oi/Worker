package core

import (
	"context"
	"crypto/subtle"
	"errors"
	"time"
)

// AdminChat это группа уведомлений админам с темами.
type AdminChat struct {
	ChatID   int64
	CodeHash []byte
	Topics   map[string]int64 // тема ядра (payments, new, errors, security) в номер темы Telegram
	BoundAt  time.Time
}

// NewChatCode выдаёт владельцу одноразовый код привязки группы. Новый код отменяет старый.
func (c *Core) NewChatCode(ctx context.Context, actorTg int64) (string, error) {
	if err := c.ownerOnly(ctx, actorTg, "chat.code"); err != nil {
		return "", err
	}
	code := randomToken()[:16]
	err := c.tx(ctx, func(tx Tx) error {
		ch, err := tx.AdminChat()
		if err != nil && !errors.Is(err, ErrNotFound) {
			return err
		}
		ch.CodeHash = sha(code)
		if err := tx.SaveAdminChat(ch); err != nil {
			return err
		}
		return c.audit(ctx, tx, actorTg, "chat.code", "", nil, c.clock.Now())
	})
	return code, err
}

// BindChat привязывает группу по коду. Привязать может только владелец, код одноразовый.
// Без привязки бот в группу ничего не шлёт.
func (c *Core) BindChat(ctx context.Context, chatID, byTg int64, code string) error {
	if !c.isOwner(byTg) {
		return ErrForbidden
	}
	return c.tx(ctx, func(tx Tx) error {
		ch, err := tx.AdminChat()
		if err != nil || len(ch.CodeHash) == 0 || subtle.ConstantTimeCompare(sha(code), ch.CodeHash) != 1 {
			return ErrForbidden
		}
		now := c.clock.Now()
		ch.ChatID, ch.CodeHash, ch.BoundAt, ch.Topics = chatID, nil, now, nil
		if err := tx.SaveAdminChat(ch); err != nil {
			return err
		}
		if err := c.audit(ctx, tx, byTg, "chat.bind", itoa(chatID), nil, now); err != nil {
			return err
		}
		return c.alert(tx, "admin.chat_bound", map[string]string{"chat": itoa(chatID)}, now)
	})
}

// AdminChat отдаёт привязанную группу. Нет привязки: ErrNotFound.
func (c *Core) AdminChat(ctx context.Context) (AdminChat, error) {
	var ch AdminChat
	err := c.store.Tx(ctx, func(tx Tx) error {
		var err error
		ch, err = tx.AdminChat()
		if err == nil && ch.ChatID == 0 {
			err = ErrNotFound
		}
		return err
	})
	return ch, err
}

// SetChatTopics запоминает номера тем, которые бот создал в группе.
func (c *Core) SetChatTopics(ctx context.Context, topics map[string]int64) error {
	return c.tx(ctx, func(tx Tx) error {
		ch, err := tx.AdminChat()
		if err != nil {
			return err
		}
		ch.Topics = topics
		return tx.SaveAdminChat(ch)
	})
}
