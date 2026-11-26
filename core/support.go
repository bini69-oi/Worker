package core

import (
	"context"
	"errors"
	"time"
)

// SupportCard это короткая карточка абонента для темы «Поддержка»: кто пишет и что у него с подпиской.
type SupportCard struct {
	User      User
	Tariff    string    // пусто, если подписки нет; у пробного «пробный»
	Trial     bool      // подписка пробная
	ExpiresAt time.Time // нулевое время, если подписки нет
	Active    bool      // подписка ещё идёт
}

// SupportCard собирает карточку абонента для поддержки. Только чтение, права не нужны:
// карточку видят админы в своей группе, абонент её не получает.
func (c *Core) SupportCard(ctx context.Context, userID int64) (SupportCard, error) {
	var card SupportCard
	u, err := c.store.User(ctx, userID)
	if err != nil {
		return card, err
	}
	card.User = u
	sub, err := c.store.SubByUser(ctx, userID)
	if errors.Is(err, ErrNotFound) {
		return card, nil
	}
	if err != nil {
		return card, err
	}
	card.Trial, card.ExpiresAt = sub.Trial, sub.ExpiresAt
	card.Active = sub.Status == SubActive && sub.ExpiresAt.After(c.clock.Now())
	if sub.TariffID != 0 {
		err := c.store.Tx(ctx, func(tx Tx) error {
			t, err := tx.Tariff(sub.TariffID)
			card.Tariff = t.Name
			return err
		})
		if err != nil && !errors.Is(err, ErrNotFound) {
			return card, err
		}
	}
	return card, nil
}

// supportKeep это сколько живёт связь «сообщение в группе, абонент»: реплай на старое не уходит.
const supportKeep = 30 * 24 * time.Hour

// LinkSupport запоминает, что сообщение chatMsgID в группе chatID относится к абоненту userID.
func (c *Core) LinkSupport(ctx context.Context, chatID, chatMsgID, userID int64) error {
	return c.tx(ctx, func(tx Tx) error { return tx.AddSupportMsg(chatID, chatMsgID, userID, c.clock.Now()) })
}

// SupportUser отдаёт абонента, которому уходит реплай на сообщение в группе.
// Связи нет или она старше 30 дней: ErrNotFound.
func (c *Core) SupportUser(ctx context.Context, chatID, chatMsgID int64) (int64, error) {
	var uid int64
	err := c.store.Tx(ctx, func(tx Tx) error {
		id, at, err := tx.SupportMsg(chatID, chatMsgID)
		if err == nil && c.clock.Now().Sub(at) > supportKeep {
			err = ErrNotFound
		}
		uid = id
		return err
	})
	return uid, err
}

// CleanSupport удаляет связи поддержки старше 30 дней. Фоновая задача раз в сутки.
func (c *Core) CleanSupport(ctx context.Context) (int, error) {
	return c.store.DeleteSupportBefore(ctx, c.clock.Now().Add(-supportKeep))
}
