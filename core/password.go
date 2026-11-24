package core

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	"golang.org/x/crypto/argon2"
)

// Параметры argon2id по рекомендации OWASP: 19 МБ памяти, 2 прохода. Перебор такого хэша очень дорог.
const (
	argonTime    = 2
	argonMemory  = 19 * 1024
	argonThreads = 1
	argonKeyLen  = 32
	minPassword  = 10
)

// hashPassword делает хэш argon2id в стандартном виде $argon2id$v=19$m=...,t=...,p=...$соль$хэш.
func hashPassword(pw string) string {
	salt := make([]byte, 16)
	_, _ = rand.Read(salt) // rand.Read в Go не возвращает ошибку
	key := argon2.IDKey([]byte(pw), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	b64 := base64.RawStdEncoding.EncodeToString
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s", argon2.Version, argonMemory, argonTime, argonThreads, b64(salt), b64(key))
}

// checkPassword сверяет пароль с хэшем за постоянное время. Параметры берутся из самого хэша.
func checkPassword(hash, pw string) bool {
	parts := strings.Split(hash, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false
	}
	var m uint32
	var t uint32
	var p uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &m, &t, &p); err != nil {
		return false
	}
	salt, err1 := base64.RawStdEncoding.DecodeString(parts[4])
	want, err2 := base64.RawStdEncoding.DecodeString(parts[5])
	if err1 != nil || err2 != nil || len(want) == 0 {
		return false
	}
	got := argon2.IDKey([]byte(pw), salt, t, m, p, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1
}

// SetOwnerPassword задаёт пароль владельца. Зовёт его только команда на сервере worker admin password:
// из бота и админки пароль владельца не меняется.
func (c *Core) SetOwnerPassword(ctx context.Context, pw string) error {
	if c.owner <= 0 {
		return ErrOwnerOnly
	}
	if len([]rune(pw)) < minPassword {
		return ErrWeakPassword
	}
	hash := hashPassword(pw)
	return c.tx(ctx, func(tx Tx) error {
		now := c.clock.Now()
		a, err := tx.Admin(c.owner)
		if err != nil {
			a = Admin{TgID: c.owner, CreatedAt: now}
		}
		a.PassHash = hash
		if err := tx.SaveAdmin(a); err != nil {
			return err
		}
		// Все сессии закрываются: старым паролем больше нельзя пользоваться ни в одном браузере.
		if err := tx.CloseAdminSessions(c.owner, now); err != nil {
			return err
		}
		if err := c.audit(ctx, tx, c.owner, "owner.password", "", nil, now); err != nil {
			return err
		}
		return c.alert(tx, "admin.password_set", nil, now)
	})
}

// Защита входа от перебора: после 5 ошибок пауза 1 минута, потом удваивается до часа.
const (
	loginTries    = 5
	loginPause    = time.Minute
	loginMaxPause = time.Hour
)

func loginLock(count int, now time.Time) time.Time {
	if count < loginTries {
		return time.Time{}
	}
	pause := loginPause << min(count-loginTries, 6)
	return now.Add(min(pause, loginMaxPause))
}
