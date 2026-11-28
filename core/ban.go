package core

import (
	"context"
	"errors"
	"strings"
	"time"
)

// maxComment это предел комментария админа к действию.
const maxComment = 500

// errSame значит, что менять нечего: бан уже стоит или уже снят. Наружу не выходит.
var errSame = errors.New("состояние уже такое")

// Ban банит абонента: бот и кабинет ему отказывают, панель выключает его через очередь.
// Нужно право ban. Повторный бан ничего не меняет и не пишется в журнал второй раз.
func (c *Core) Ban(ctx context.Context, actorTg, userID int64, comment string) error {
	return c.setBan(ctx, actorTg, userID, true, comment)
}

// Unban снимает бан. Панель включает абонента обратно, если подписка ещё идёт.
func (c *Core) Unban(ctx context.Context, actorTg, userID int64, comment string) error {
	return c.setBan(ctx, actorTg, userID, false, comment)
}

func (c *Core) setBan(ctx context.Context, actorTg, userID int64, on bool, comment string) error {
	action := "user.unban"
	if on {
		action = "user.ban"
	}
	comment = strings.TrimSpace(comment)
	if len([]rune(comment)) > maxComment {
		return ErrTooLong
	}
	err := c.asAdmin(ctx, actorTg, PermBan, action, func(tx Tx, a Admin, now time.Time) (string, map[string]string, error) {
		u, err := tx.User(userID)
		if err != nil {
			return "", nil, err
		}
		// Владельца не банит никто, админа банит только владелец: помощник не выключит VPN команде.
		if _, staff := c.admin(tx, u.TgID); c.isOwner(u.TgID) || (staff == nil && !a.Owner) {
			return "", nil, ErrForbidden
		}
		if !u.BannedAt.IsZero() == on {
			return "", nil, errSame
		}
		at := time.Time{}
		if on {
			at = now
		}
		if err := tx.SetBanned(userID, at); err != nil {
			return "", nil, err
		}
		if on { // бан закрывает все сессии кабинета сразу
			if err := tx.CloseUserSessions(userID, now); err != nil {
				return "", nil, err
			}
		}
		if sub, err := tx.SubByUser(userID); err == nil {
			if err := enqueueSync(tx, sub.ID, now); err != nil {
				return "", nil, err
			}
		} else if !errors.Is(err, ErrNotFound) {
			return "", nil, err
		}
		return userTarget(userID), map[string]string{"comment": comment}, nil
	})
	if errors.Is(err, errSame) {
		return nil
	}
	return err
}
