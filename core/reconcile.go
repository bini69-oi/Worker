package core

import (
	"context"
	"strconv"
	"time"
)

// Reconcile сверяет базу с панелью. Если абонент в панели расходится с базой (кто-то поправил его руками),
// ставится задача вернуть панель к базе, а админ получает одно сообщение о расхождениях.
// Возвращает число расхождений.
func (c *Core) Reconcile(ctx context.Context) (int, error) {
	subs, err := c.store.SubsInPanel(ctx)
	if err != nil {
		return 0, err
	}
	now := c.clock.Now()
	var diff []int64
	for _, s := range subs {
		got, err := c.panel.Get(ctx, s.PanelUUID)
		if err != nil {
			c.log.Warn("сверка: панель не ответила", "sub", s.ID, "err", err)
			continue
		}
		if differs(s, got, now) {
			c.log.Warn("сверка: панель расходится с базой", "sub", s.ID,
				"db_expire", s.ExpiresAt, "panel_expire", got.ExpireAt, "panel_enabled", got.Enabled)
			diff = append(diff, s.ID)
		}
	}
	if len(diff) == 0 {
		return 0, nil
	}
	err = c.tx(ctx, func(tx Tx) error {
		for _, id := range diff {
			if err := enqueueSync(tx, id, now); err != nil {
				return err
			}
		}
		return enqueueAdmin(tx, AdminTask{Topic: TopicErrors, Key: "admin.panel_diff",
			Vars: map[string]string{"count": strconv.Itoa(len(diff))}}, now)
	})
	return len(diff), err
}

// differs говорит, что панель не совпадает с базой. Срок сравнивается только у живой подписки:
// у отключённой панель может сдвигать срок из прошлого вперёд.
func differs(s Subscription, got PanelResult, now time.Time) bool {
	want := s.Status == SubActive && s.ExpiresAt.After(now)
	if got.Enabled != want {
		return true
	}
	if !want {
		return false
	}
	d := got.ExpireAt.Sub(s.ExpiresAt)
	return d > time.Minute || d < -time.Minute
}
