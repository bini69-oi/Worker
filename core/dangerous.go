package core

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/bini69-oi/worker/settings"
	"slices"
	"time"
)

// ConfigWriter это запись настроек. Нужна ядру только для опасных действий: смены ключей и токена.
type ConfigWriter interface {
	PrepareSecret(key, raw string) (settings.Change, error)
	Apply(ch settings.Change)
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
	// Хвост ключа нужен только в сообщении владельцу. В журнал действий, который хранится вечно, он не идёт.
	logged := map[string]string{}
	for k, v := range vars {
		if k != "tail" {
			logged[k] = v
		}
	}
	var cf Confirm
	err = c.tx(ctx, func(tx Tx) error {
		now := c.clock.Now()
		if cf, err = c.newConfirm(tx, c.owner, kind, body, requestVars(ctx, actorTg, vars), now); err != nil {
			return err
		}
		return c.audit(ctx, tx, actorTg, "request:"+kind, itoa(cf.ID), logged, now)
	})
	if err == nil && secret != "" {
		c.secrets.put(cf.ID, secret, cf.ExpiresAt, c.clock.Now())
	}
	return cf.ID, err
}

// requestVars дополняет сообщение владельцу: кто просит и с какого IP. Владелец видит, на что жмёт «Да».
func requestVars(ctx context.Context, actorTg int64, vars map[string]string) map[string]string {
	out := map[string]string{"by": itoa(actorTg), "ip": ipOf(ctx)}
	for k, v := range vars {
		out[k] = v
	}
	return out
}

// tail отдаёт последние 4 символа длинного секрета: по ним владелец узнаёт свой ключ.
func tail(v string) string {
	r := []rune(v)
	if len(r) < 16 {
		return ""
	}
	return string(r[len(r)-4:])
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
	return c.request(ctx, actorTg, "secret", secretPayload{Key: key}, map[string]string{"key": key, "tail": tail(value)}, value)
}

// RequestBotToken просит заменить токен бота. Новый токен сначала проверяется в Telegram:
// неверный не сохраняется, бот продолжает работать на старом.
func (c *Core) RequestBotToken(ctx context.Context, actorTg int64, token string) (int64, error) {
	if err := c.ownerOnly(ctx, actorTg, "bot_token"); err != nil {
		return 0, err
	}
	// Токен уходит в адрес запроса к Telegram и может оказаться в тексте ошибки: в фильтр журнала его кладём заранее.
	c.rememberSecret(token)
	if c.telegram == nil || c.telegram.CheckToken(ctx, token) != nil {
		return 0, ErrBadToken
	}
	return c.request(ctx, actorTg, "bot_token", struct{}{}, map[string]string{"tail": tail(token)}, token)
}

// RequestAddHelper просит добавить помощника с правами и паролем. Пароль хэшируется сразу,
// в подтверждении лежит только хэш.
func (c *Core) RequestAddHelper(ctx context.Context, actorTg, helperTg int64, perms []Perm, password string) (int64, error) {
	if err := c.ownerOnly(ctx, actorTg, "admins.add"); err != nil {
		return 0, err
	}
	if helperTg <= 0 || c.isOwner(helperTg) || len([]rune(password)) < minPassword || len(perms) == 0 {
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

// action это внешнее опасное действие: какое право нужно автору и кто его выполняет.
type action struct {
	perm Perm
	fn   func(ctx context.Context, payload []byte) error
}

// actionPayload это запрос внешнего действия: кто просит и с чем. Автор нужен, чтобы после «Да»
// проверить его права ещё раз: за 2 минуты помощника могли отключить.
type actionPayload struct {
	By   int64           `json:"by"`
	Data json.RawMessage `json:"data"`
}

// maxDetails это предел текста действия. Владелец видит в Telegram всё, что подтверждает, без обрезки,
// а длиннее сообщение Telegram не уместит.
const maxDetails = 3000

// OnApproved подключает исполнителя для опасного действия, которое ядро само не делает
// (выгрузка базы, рассылка всем), и право, которое нужно, чтобы его попросить. Зовётся при старте.
func (c *Core) OnApproved(kind string, perm Perm, fn func(ctx context.Context, payload []byte) error) {
	c.actions[kind] = action{perm: perm, fn: fn}
}

// RequestAction просит владельца подтвердить внешнее опасное действие (выгрузка базы, рассылка всем).
// Владелец видит данные действия целиком: что видит, то и подтверждает.
func (c *Core) RequestAction(ctx context.Context, actorTg int64, kind string, payload any) (int64, error) {
	act, ok := c.actions[kind]
	if !ok {
		return 0, ErrInvalid
	}
	denied := false
	err := c.store.Tx(ctx, func(tx Tx) error {
		_, err := c.can(ctx, tx, actorTg, act.perm)
		denied = errors.Is(err, ErrForbidden)
		return err
	})
	if denied {
		c.deny(ctx, actorTg, kind)
	}
	if err != nil {
		return 0, err
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return 0, err
	}
	if len([]rune(string(data))) > maxDetails {
		return 0, ErrTooLong
	}
	return c.request(ctx, actorTg, kind, actionPayload{By: actorTg, Data: data}, map[string]string{"details": string(data)}, "")
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

// saveSecret записывает подтверждённый секрет: строка в базе и запись в журнал одной транзакцией, потом кэш.
// В журнале только имя ключа.
func (c *Core) saveSecret(ctx context.Context, w ConfigWriter, key, value string) error {
	ch, err := w.PrepareSecret(key, value)
	if err != nil {
		return settingErr(err)
	}
	err = c.asAdmin(ctx, c.owner, PermOwner, "secret.set", func(tx Tx, _ Admin, now time.Time) (string, map[string]string, error) {
		if ch.Delete {
			err = tx.DeleteSetting(ch.Key)
		} else {
			err = tx.SaveSetting(ch.Key, ch.Stored, now)
		}
		return key, nil, err
	})
	if err == nil {
		w.Apply(ch)
	}
	return err
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
		c.rememberSecret(value)
		return c.saveSecret(ctx, w, p.Key, value)
	case "bot_token":
		token, ok := c.secrets.take(cf.ID, now)
		w, can := c.cfg.(ConfigWriter)
		if !ok || !can || c.telegram == nil {
			return ErrConfirmGone
		}
		c.rememberSecret(token)
		if err := c.telegram.SetWebhook(ctx, token); err != nil {
			// Текст ошибки может содержать адрес с токеном: наружу только код, подробности в журнал через фильтр.
			c.log.Error("вебхук на новый токен не поставлен", "err", err)
			return ErrBadToken
		}
		return c.saveSecret(ctx, w, "bot.token", token)
	case "add_admin", "remove_admin":
		var p adminPayload
		if err := json.Unmarshal(cf.Payload, &p); err != nil {
			return err
		}
		return c.tx(ctx, func(tx Tx) error { return c.changeAdmin(tx, cf.Kind, p, now) })
	}
	act, ok := c.actions[cf.Kind]
	if !ok {
		return ErrInvalid
	}
	var p actionPayload
	if err := json.Unmarshal(cf.Payload, &p); err != nil {
		return err
	}
	if err := c.store.Tx(ctx, func(tx Tx) error { _, err := c.can(ctx, tx, p.By, act.perm); return err }); err != nil {
		return err
	}
	return act.fn(ctx, p.Data)
}

// rememberSecret добавляет новый секрет в фильтр журнала до того, как с ним что-то случится.
func (c *Core) rememberSecret(v string) {
	if c.filter != nil {
		c.filter.Add(v)
	}
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
