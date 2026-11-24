package core

import (
	"context"
	"errors"
	"strconv"
	"strings"
)

// PanelEvent это событие из вебхука панели, уже проверенное по подписи.
type PanelEvent struct {
	Scope    string // user, node, service и т.п.
	Name     string // например user.limited
	TgID     int64
	Username string
}

// Имена событий панели, на которые ядро отвечает.
const (
	EventTrafficLimited = "user.limited"
	ScopeNode           = "node"
)

// HandlePanelEvent отвечает на событие панели. Абонент, у которого кончился трафик, получает предложение
// продлить (не чаще раза в день). События нод уходят админам. Остальное ядро не трогает:
// сроки и включение абонента ведёт база.
func (c *Core) HandlePanelEvent(ctx context.Context, ev PanelEvent) error {
	now := c.clock.Now()
	switch {
	case ev.Scope == ScopeNode:
		return c.tx(ctx, func(tx Tx) error {
			return enqueueAdmin(tx, AdminTask{Topic: TopicErrors, Key: "admin.panel_event",
				Vars: map[string]string{"event": ev.Name}}, now)
		})
	case ev.Name == EventTrafficLimited:
		return c.tx(ctx, func(tx Tx) error {
			u, err := eventUser(tx, ev)
			if errors.Is(err, ErrNotFound) {
				return nil // абонент не наш
			}
			if err != nil {
				return err
			}
			fresh, err := tx.AddNotice(u.ID, "traffic_limited", now.Format("2006-01-02"), now)
			if err != nil || !fresh {
				return err
			}
			return enqueueNotify(tx, NotifyTask{UserID: u.ID, Key: "notify.traffic_limited"}, now)
		})
	}
	return nil
}

// eventUser находит абонента события: по Telegram ID, а если его нет, по имени вида u<Telegram ID>.
func eventUser(tx Tx, ev PanelEvent) (User, error) {
	tg := ev.TgID
	if tg == 0 {
		n, err := strconv.ParseInt(strings.TrimPrefix(ev.Username, "u"), 10, 64)
		if err != nil || !strings.HasPrefix(ev.Username, "u") {
			return User{}, ErrNotFound
		}
		tg = n
	}
	return tx.UserByTg(tg)
}
