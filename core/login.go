package core

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"strings"
	"sync"
	"time"
	"unicode"
)

// Сроки входа и сессии.
const (
	sessionTTL = 12 * time.Hour
	finishTTL  = 5 * time.Minute // за сколько после начала входа его надо закончить
)

// LoginInfo это откуда входят: показывается админу в сообщении о входе и пишется в журнал.
type LoginInfo struct {
	IP    string
	Agent string
}

// Session это сессия админки. В базе хранится только SHA-256 токена, сам токен знает лишь браузер.
type Session struct {
	ID        string // начало хэша токена, по нему владелец закрывает сессию
	AdminTg   int64
	IP        string
	Agent     string
	CreatedAt time.Time
	ExpiresAt time.Time
	ClosedAt  time.Time
}

type loginPayload struct {
	IP        string `json:"ip"`
	Agent     string `json:"agent"`
	NonceHash string `json:"nonce_hash"`
}

func randomToken() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b) // rand.Read в Go не возвращает ошибку
	return base64.RawURLEncoding.EncodeToString(b)
}

func sha(s string) []byte {
	h := sha256.Sum256([]byte(s))
	return h[:]
}

// cleanInfo обрезает IP и браузер и убирает управляющие символы: эти строки приходят от клиента
// и попадают в сообщение админу и в журнал.
func cleanInfo(in LoginInfo) LoginInfo {
	clean := func(s string, n int) string {
		r := []rune(strings.Map(func(r rune) rune {
			if unicode.IsControl(r) {
				return -1
			}
			return r
		}, s))
		return string(r[:min(len(r), n)])
	}
	return LoginInfo{IP: clean(in.IP, 64), Agent: clean(in.Agent, 200)}
}

// dummyHash нужен, чтобы вход незнакомого админа считал хэш так же долго, как знакомого,
// и по времени ответа нельзя было подобрать номера админов.
var dummyHash = sync.OnceValue(func() string { return hashPassword(randomToken()) })

// LoginStart это первый шаг входа: пароль. При верном пароле админу в Telegram уходит «Вход с IP, браузер
// [Да] [Нет]». Возвращает номер подтверждения и одноразовый ключ браузера: без ключа закончить вход нельзя,
// даже зная номер. Неверный логин и неверный пароль дают одну и ту же ошибку.
//
// Попытка засчитывается до проверки пароля и своей транзакцией, поэтому пачка параллельных запросов
// счётчик не обходит. Блокируется пара «админ + IP» и сам IP: чужой не может заблокировать вход владельцу,
// а подбор пароля без кнопки в Telegram всё равно ничего не даёт.
func (c *Core) LoginStart(ctx context.Context, tgID int64, password string, info LoginInfo) (int64, string, error) {
	info = cleanInfo(info)
	now := c.clock.Now()
	keys := []string{"login:" + itoa(tgID) + "@" + info.IP, "loginip:" + info.IP}
	var a Admin
	known, alarm := false, false
	err := c.tx(ctx, func(tx Tx) error {
		for _, k := range keys {
			count, until, err := tx.Limit(k)
			if err != nil {
				return err
			}
			if count >= loginTries && now.Before(until) {
				return ErrLoginLocked
			}
			count++
			if err := tx.SetLimit(k, count, loginLock(count, now)); err != nil {
				return err
			}
			alarm = alarm || count == loginTries
		}
		var err error
		a, err = c.admin(tx, tgID)
		known = err == nil
		return nil
	})
	if err != nil {
		return 0, "", err
	}
	if known && a.Owner && a.PassHash == "" {
		c.log.Warn("пароль владельца не задан, выполните на сервере: worker admin password")
		return 0, "", ErrNoPassword
	}

	// Медленный хэш считается вне транзакции записи: иначе на это время встали бы все записи, включая оплаты.
	hash := a.PassHash
	if !known {
		hash = dummyHash()
	}
	if !checkPassword(hash, password) || !known {
		if alarm {
			_ = c.tx(ctx, func(tx Tx) error {
				return c.alert(tx, "admin.login_bruteforce", map[string]string{"admin": itoa(tgID), "ip": info.IP}, now)
			})
		}
		return 0, "", ErrBadLogin
	}

	nonce := randomToken()
	var confirmID int64
	err = c.tx(ctx, func(tx Tx) error {
		for _, k := range keys {
			if err := tx.SetLimit(k, 0, now); err != nil {
				return err
			}
		}
		body, _ := json.Marshal(loginPayload{IP: info.IP, Agent: info.Agent, NonceHash: hex.EncodeToString(sha(nonce))})
		cf, err := c.newConfirm(tx, tgID, "login", body, map[string]string{"ip": info.IP, "agent": info.Agent}, now)
		confirmID = cf.ID
		return err
	})
	return confirmID, nonce, err
}

// LoginFinish это второй шаг: админ нажал «Да». Браузер предъявляет номер подтверждения и свой ключ
// и получает токен сессии на 12 часов. Подтверждение одноразовое.
func (c *Core) LoginFinish(ctx context.Context, confirmID int64, nonce string, info LoginInfo) (string, Session, error) {
	info = cleanInfo(info)
	token := randomToken()
	var s Session
	err := c.tx(ctx, func(tx Tx) error {
		now := c.clock.Now()
		cf, err := tx.Confirm(confirmID)
		if err != nil || cf.Kind != "login" || now.Sub(cf.CreatedAt) > finishTTL {
			return ErrNotConfirmed
		}
		var p loginPayload
		if err := json.Unmarshal(cf.Payload, &p); err != nil {
			return err
		}
		if subtle.ConstantTimeCompare([]byte(hex.EncodeToString(sha(nonce))), []byte(p.NonceHash)) != 1 {
			return ErrNotConfirmed
		}
		if cf.Answer != "yes" {
			return ErrNotConfirmed
		}
		if ok, err := tx.UseConfirm(confirmID, now); err != nil || !ok {
			if err == nil {
				err = ErrConfirmGone
			}
			return err
		}
		hash := sha(token)
		s = Session{ID: hex.EncodeToString(hash[:8]), AdminTg: cf.AdminTg, IP: info.IP, Agent: info.Agent,
			CreatedAt: now, ExpiresAt: now.Add(sessionTTL)}
		if err := tx.CreateSession(hash, s); err != nil {
			return err
		}
		if err := c.audit(WithIP(ctx, info.IP), tx, cf.AdminTg, "login", "", map[string]string{"agent": info.Agent}, now); err != nil {
			return err
		}
		return c.alert(tx, "admin.login", map[string]string{"admin": itoa(cf.AdminTg), "ip": info.IP, "agent": info.Agent}, now)
	})
	if err != nil {
		return "", Session{}, err
	}
	return token, s, nil
}

// SessionAdmin проверяет токен сессии на каждом запросе админки и отдаёт админа с его правами сейчас.
func (c *Core) SessionAdmin(ctx context.Context, token string) (Admin, Session, error) {
	var a Admin
	var s Session
	err := c.store.Tx(ctx, func(tx Tx) error {
		var err error
		s, err = tx.Session(sha(token))
		if err != nil || !s.ClosedAt.IsZero() || !c.clock.Now().Before(s.ExpiresAt) {
			return ErrSessionGone
		}
		a, err = c.admin(tx, s.AdminTg)
		if err != nil {
			return ErrSessionGone
		}
		return nil
	})
	return a, s, err
}

// Sessions отдаёт открытые сессии: владельцу все, помощнику только свои.
func (c *Core) Sessions(ctx context.Context, actorTg int64) ([]Session, error) {
	all, err := c.store.Sessions(ctx, c.clock.Now())
	if err != nil || c.isOwner(actorTg) {
		return all, err
	}
	var own []Session
	for _, s := range all {
		if s.AdminTg == actorTg {
			own = append(own, s)
		}
	}
	return own, nil
}

// CloseSession закрывает сессию. Свою может закрыть любой, чужую только владелец.
func (c *Core) CloseSession(ctx context.Context, actorTg int64, id string) error {
	list, err := c.store.Sessions(ctx, c.clock.Now())
	if err != nil {
		return err
	}
	for _, s := range list {
		if s.ID != id {
			continue
		}
		if s.AdminTg != actorTg && !c.isOwner(actorTg) {
			return ErrOwnerOnly
		}
		return c.tx(ctx, func(tx Tx) error {
			now := c.clock.Now()
			if err := tx.CloseSession(id, now); err != nil {
				return err
			}
			return c.audit(ctx, tx, actorTg, "session.close", id, map[string]string{"admin": itoa(s.AdminTg)}, now)
		})
	}
	return ErrNotFound
}
