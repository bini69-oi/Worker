package core

import (
	"context"
	"encoding/hex"
	"strings"
	"time"
)

// SessionKind это вид сессии абонента: от него зависят срок и простой.
type SessionKind string

const (
	SessionTg  SessionKind = "tg"  // Mini App: 24 часа
	SessionWeb SessionKind = "web" // браузер: 30 дней, 7 дней без запросов
)

// Сроки сессий абонента.
const (
	tgSessionTTL   = 24 * time.Hour
	webSessionTTL  = 30 * day
	webSessionIdle = 7 * day
	// touchEvery это как часто запрос обновляет время последнего движения сессии: не на каждый запрос писать в базу.
	touchEvery = time.Minute
)

// UserSession это сессия абонента в кабинете. В базе только SHA-256 токена, сам токен знает лишь страница.
type UserSession struct {
	ID        string // начало хэша токена
	UserID    int64
	Kind      SessionKind
	IP        string
	Agent     string
	CreatedAt time.Time
	SeenAt    time.Time
	ExpiresAt time.Time
	ClosedAt  time.Time
}

// CabinetTx это действия хранилища для кабинета: сессии абонентов и входы в браузере через бота.
type CabinetTx interface {
	CreateUserSession(tokenHash []byte, s UserSession) error
	UserSession(tokenHash []byte) (UserSession, error)
	TouchUserSession(tokenHash []byte, at time.Time) error
	CloseUserSession(tokenHash []byte, at time.Time) error
	// CloseUserSessions закрывает все открытые сессии абонента.
	CloseUserSessions(userID int64, at time.Time) error

	CreateUserLogin(l UserLogin) (int64, error)
	UserLogin(id int64) (UserLogin, error)
	UserLoginByHash(idHash []byte) (UserLogin, error)
	UserLoginByNonce(nonceHash []byte) (UserLogin, error)
	SaveUserLogin(l UserLogin) error
	// PruneCabinet стирает закрытые и истёкшие сессии и старые запросы входа, которым срок вышел до before.
	PruneCabinet(before time.Time) error
}

// OpenSession выдаёт токен сессии абоненту. Забаненному сессия не выдаётся.
func (c *Core) OpenSession(ctx context.Context, userID int64, kind SessionKind, info LoginInfo) (string, UserSession, error) {
	var token string
	var s UserSession
	err := c.tx(ctx, func(tx Tx) error {
		var err error
		token, s, err = c.openSessionTx(tx, userID, kind, info)
		return err
	})
	if err != nil {
		return "", UserSession{}, err
	}
	return token, s, nil
}

func (c *Core) openSessionTx(tx Tx, userID int64, kind SessionKind, info LoginInfo) (string, UserSession, error) {
	info = cleanInfo(info)
	if _, err := activeUser(tx, userID); err != nil {
		return "", UserSession{}, err
	}
	now := c.clock.Now()
	ttl := tgSessionTTL
	if kind == SessionWeb {
		ttl = webSessionTTL
	}
	token := randomToken()
	hash := sha(token)
	s := UserSession{ID: hex.EncodeToString(hash[:8]), UserID: userID, Kind: kind, IP: info.IP, Agent: info.Agent,
		CreatedAt: now, SeenAt: now, ExpiresAt: now.Add(ttl)}
	if err := tx.CreateUserSession(hash, s); err != nil {
		return "", UserSession{}, err
	}
	// Заодно чистим то, что давно не нужно: отдельной задачи для этого нет.
	return token, s, tx.PruneCabinet(now.Add(-webSessionTTL))
}

// SessionUser проверяет токен на каждом запросе кабинета и отдаёт абонента. Закрытая, истёкшая и забытая
// (браузер без запросов 7 дней) сессия и неизвестный токен дают одну и ту же ErrSessionGone, забаненный ErrBanned.
func (c *Core) SessionUser(ctx context.Context, token string) (User, UserSession, error) {
	var u User
	var s UserSession
	if token == "" || len(token) > 128 {
		return u, s, ErrSessionGone
	}
	err := c.tx(ctx, func(tx Tx) error {
		now := c.clock.Now()
		hash := sha(token)
		var err error
		if s, err = tx.UserSession(hash); err != nil {
			return ErrSessionGone
		}
		if u, err = tx.User(s.UserID); err != nil {
			return ErrSessionGone
		}
		// Забаненный получает отказ на любой запрос со своим токеном, даже когда бан уже закрыл его сессии.
		if !u.BannedAt.IsZero() {
			return ErrBanned
		}
		if !s.ClosedAt.IsZero() || !now.Before(s.ExpiresAt) || (s.Kind == SessionWeb && now.Sub(s.SeenAt) > webSessionIdle) {
			return ErrSessionGone
		}
		if now.Sub(s.SeenAt) >= touchEvery {
			if err := tx.TouchUserSession(hash, now); err != nil {
				return err
			}
			s.SeenAt = now
		}
		return nil
	})
	return u, s, err
}

// Logout закрывает сессию с этим токеном. Неизвестный токен не ошибка: выйти из выхода нельзя.
func (c *Core) Logout(ctx context.Context, token string) error {
	return c.tx(ctx, func(tx Tx) error { return tx.CloseUserSession(sha(token), c.clock.Now()) })
}

// LogoutAll закрывает все сессии абонента, на всех устройствах.
func (c *Core) LogoutAll(ctx context.Context, userID int64) error {
	return c.tx(ctx, func(tx Tx) error { return tx.CloseUserSessions(userID, c.clock.Now()) })
}

// TgLogin это вход из Mini App после проверки подписи: находит абонента по Telegram ID или регистрирует
// (как /start, с реферальным кодом из start_param вида ref_<код>) и выдаёт сессию Telegram.
// Подпись и свежесть initData проверяет вызывающий: ядро не знает токена бота.
func (c *Core) TgLogin(ctx context.Context, tgID int64, username, langCode, startParam string, info LoginInfo) (string, User, error) {
	ref := ""
	if code, ok := strings.CutPrefix(startParam, "ref_"); ok && len(code) <= 32 && isWord(code) {
		ref = code
	}
	u, err := c.RegisterRef(ctx, tgID, username, c.Lang(langCode), ref)
	if err != nil {
		return "", User{}, err
	}
	token, _, err := c.OpenSession(ctx, u.ID, SessionTg, info)
	if err != nil {
		return "", User{}, err
	}
	return token, u, nil
}

// isWord говорит, что s не пусто и состоит из латиницы, цифр, `_` и `-`: так выглядит реферальный код.
func isWord(s string) bool {
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-') {
			return false
		}
	}
	return s != ""
}
