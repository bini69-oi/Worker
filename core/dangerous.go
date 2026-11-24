package core

import (
	"context"
	"encoding/json"
	"slices"
	"time"
)

// ConfigWriter это запись настроек. Нужна ядру только для опасных действий: смены ключей и токена.
type ConfigWriter interface {
	Set(key, raw string) error
	IsSecret(key string) bool
}

// TelegramAPI это то, что ядру нужно от Telegram при смене токена бота. Реализует бот.
type TelegramAPI interface {
	// CheckToken спрашивает Telegram, рабочий ли токен (метод getMe).
	CheckToken(ctx context.Context, token string) error
	// SetWebhook ставит вебхук на новый токен.
	SetWebhook(ctx context.Context, token string) error
}

type secretPayload struct {
	Key string `json:"key"`
}

type adminPayload struct {
	TgID  int64  `json:"tg_id"`
	Perms []Perm `json:"perms,omitempty"`
	Hash  string `json:"hash,omitempty"`
}

// request создаёт подтверждение опасного действия для владельца. Само действие выполнится после «Да».
func (c *Core) request(ctx context.Context, actorTg int64, kind string, payload any, vars map[string]string, secret string) (int64, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return 0, err
	}
	var cf Confirm
	err = c.tx(ctx, func(tx Tx) error {
		now := c.clock.Now()
		if cf, err = c.newConfirm(tx, c.owner, kind, body, vars, now); err != nil {
			return err
		}
		return c.audit(ctx, tx, actorTg, "request:"+kind, itoa(cf.ID), vars, now)
	})
	if err == nil && secret != "" {
		c.secrets.put(cf.ID, secret, cf.ExpiresAt)
	}
	return cf.ID, err
}

// RequestSecret просит сменить секретную настройку (ключ кассы, секрет вебхука). Только владелец,
// и только после его «Да» в Telegram: угнанная сессия одна ключ не поменяет.
func (c *Core) RequestSecret(ctx context.Context, actorTg int64, key, value string) (int64, error) {
	if err := c.ownerOnly(ctx, actorTg, "secret:"+key); err != nil {
		return 0, err
	}
	w, ok := c.cfg.(ConfigWriter)
	if !ok || !w.IsSecret(key) || value == "" {
		return 0, ErrInvalid
	}
	return c.request(ctx, actorTg, "secret", secretPayload{Key: key}, map[string]string{"key": key}, value)
}

// RequestBotToken просит заменить токен бота. Новый токен сначала проверяется в Telegram:
// неверный не сохраняется, бот продолжает работать на старом.
func (c *Core) RequestBotToken(ctx context.Context, actorTg int64, token string) (int64, error) {
	if err := c.ownerOnly(ctx, actorTg, "bot_token"); err != nil {
		return 0, err
	}
	if c.telegram == nil || c.telegram.CheckToken(ctx, token) != nil {
		return 0, ErrBadToken
	}
	return c.request(ctx, actorTg, "bot_token", struct{}{}, nil, token)
}

// RequestAddHelper просит добавить помощника с правами и паролем. Пароль хэшируется сразу,
// в подтверждении лежит только хэш.
func (c *Core) RequestAddHelper(ctx context.Context, actorTg, helperTg int64, perms []Perm, password string) (int64, error) {
	if err := c.ownerOnly(ctx, actorTg, "admins.add"); err != nil {
		return 0, err
	}
	if helperTg <= 0 || c.isOwner(helperTg) || len([]rune(password)) < minPassword {
		return 0, ErrInvalid
	}
	for _, p := range perms {
		if !slices.Contains(HelperPerms, p) {
			return 0, ErrInvalid
		}
	}
	return c.request(ctx, actorTg, "add_admin", adminPayload{TgID: helperTg, Perms: perms, Hash: hashPassword(password)},
		map[string]string{"admin": itoa(helperTg)}, "")
}

// RequestRemoveHelper просит отключить помощника. Все его сессии закрываются.
func (c *Core) RequestRemoveHelper(ctx context.Context, actorTg, helperTg int64) (int64, error) {
	if err := c.ownerOnly(ctx, actorTg, "admins.remove"); err != nil {
		return 0, err
	}
	return c.request(ctx, actorTg, "remove_admin", adminPayload{TgID: helperTg}, map[string]string{"admin": itoa(helperTg)}, "")
}

// OnApproved подключает исполнителя для опасного действия, которое ядро само не делает
// (выгрузка базы, рассылка всем). Зовётся при старте.
func (c *Core) OnApproved(kind string, fn func(ctx context.Context, payload []byte) error) {
	c.actions[kind] = fn
}

// RequestAction просит владельца подтвердить внешнее опасное действие (выгрузка базы, рассылка всем).
func (c *Core) RequestAction(ctx context.Context, actorTg int64, kind string, perm Perm, payload any) (int64, error) {
	if _, ok := c.actions[kind]; !ok {
		return 0, ErrInvalid
	}
	err := c.store.Tx(ctx, func(tx Tx) error { _, err := c.can(ctx, tx, actorTg, perm); return err })
	if err != nil {
		return 0, err
	}
	return c.request(ctx, actorTg, kind, payload, map[string]string{"admin": itoa(actorTg)}, "")
}

// runApproved выполняет опасное действие после «Да» владельца. Идёт после транзакции подтверждения:
// запись настроек использует ту же базу и не должна ждать открытую транзакцию.
func (c *Core) runApproved(ctx context.Context, cf Confirm) error {
	now := c.clock.Now()
	err := c.execute(ctx, cf, now)
	if err != nil {
		c.log.Error("подтверждённое действие не выполнилось", "kind", cf.Kind, "confirm", cf.ID, "err", err)
		return err
	}
	return c.tx(ctx, func(tx Tx) error {
		return c.alert(tx, "admin.changed", map[string]string{"kind": cf.Kind}, now)
	})
}

func (c *Core) execute(ctx context.Context, cf Confirm, now time.Time) error {
	switch cf.Kind {
	case "secret":
		var p secretPayload
		if err := json.Unmarshal(cf.Payload, &p); err != nil {
			return err
		}
		value, ok := c.secrets.take(cf.ID, now)
		w, can := c.cfg.(ConfigWriter)
		if !ok || !can {
			return ErrConfirmGone
		}
		return w.Set(p.Key, value)
	case "bot_token":
		token, ok := c.secrets.take(cf.ID, now)
		w, can := c.cfg.(ConfigWriter)
		if !ok || !can || c.telegram == nil {
			return ErrConfirmGone
		}
		if err := c.telegram.SetWebhook(ctx, token); err != nil {
			return err
		}
		return w.Set("bot.token", token)
	case "add_admin", "remove_admin":
		var p adminPayload
		if err := json.Unmarshal(cf.Payload, &p); err != nil {
			return err
		}
		return c.tx(ctx, func(tx Tx) error { return c.changeAdmin(tx, cf.Kind, p, now) })
	}
	if fn, ok := c.actions[cf.Kind]; ok {
		return fn(ctx, cf.Payload)
	}
	return ErrInvalid
}

func (c *Core) changeAdmin(tx Tx, kind string, p adminPayload, now time.Time) error {
	if kind == "remove_admin" {
		a, err := tx.Admin(p.TgID)
		if err != nil {
			return err
		}
		a.DisabledAt = now
		if err := tx.SaveAdmin(a); err != nil {
			return err
		}
		return tx.CloseAdminSessions(p.TgID, now)
	}
	return tx.SaveAdmin(Admin{TgID: p.TgID, PassHash: p.Hash, Perms: p.Perms, CreatedAt: now})
}
