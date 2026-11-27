package core

import (
	"context"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
)

// Состояния входа для страницы, которая опрашивает его.
const (
	AdminLoginPending = "pending" // админ ещё не нажал кнопку в Telegram
	AdminLoginDenied  = "denied"  // нажат чужой код или «Это не я»
	AdminLoginExpired = "expired" // время на подтверждение вышло
	AdminLoginOK      = "ok"      // вход подтверждён, выдан токен
)

// LoginPoll это опрос входа с экрана: в каком состоянии подтверждение. Подтверждён: заканчивает вход
// (LoginFinish) и отдаёт токен сессии. Чужой ключ браузера даёт ErrNotConfirmed, как и в LoginFinish.
func (c *Core) LoginPoll(ctx context.Context, confirmID int64, nonce string, info LoginInfo) (string, string, Session, error) {
	state := ""
	err := c.store.Tx(ctx, func(tx Tx) error {
		cf, err := tx.Confirm(confirmID)
		if err != nil || cf.Kind != "login" {
			return ErrNotConfirmed
		}
		var p loginPayload
		if err := json.Unmarshal(cf.Payload, &p); err != nil {
			return ErrNotConfirmed
		}
		if subtle.ConstantTimeCompare([]byte(hex.EncodeToString(sha(nonce))), []byte(p.NonceHash)) != 1 {
			return ErrNotConfirmed
		}
		now := c.clock.Now()
		switch {
		case now.Sub(cf.CreatedAt) > finishTTL || !cf.UsedAt.IsZero():
			state = AdminLoginExpired
		case cf.Answer == "no":
			state = AdminLoginDenied
		case cf.Answer == "" && !now.Before(cf.ExpiresAt):
			state = AdminLoginExpired
		case cf.Answer == "":
			state = AdminLoginPending
		default:
			state = AdminLoginOK
		}
		return nil
	})
	if err != nil || state != AdminLoginOK {
		return state, "", Session{}, err
	}
	token, s, err := c.LoginFinish(ctx, confirmID, nonce, info)
	if err != nil {
		return "", "", Session{}, err
	}
	return AdminLoginOK, token, s, nil
}
