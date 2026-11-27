package api

import (
	"crypto/sha256"
	"encoding/hex"
	"mime"
	"net/http"
	"strings"

	"github.com/bini69-oi/worker/core"
	"github.com/bini69-oi/worker/guard"
)

// SessionCookie это имя cookie сессии. Кабинет и админка ставят её на свои пути (/api/app, /api/admin).
const SessionCookie = "worker_session"

// sessionRate это предел запросов в секунду на одну сессию. Живому человеку хватает с запасом,
// скрипт, который долбит API, упирается в него.
const sessionRate = 10

// admit это проверки, общие для каждого запроса: частота и защита от подделки.
func (s *Server) admit(r *http.Request) error {
	if !s.limit.Allow(sessionKey(r)) {
		return core.ErrTooManyTries
	}
	return checkOrigin(r)
}

// sessionKey это то, по чему считается частота: хэш токена сессии (заголовок или cookie), а без сессии IP.
// Хэш, чтобы сам токен не лежал в памяти счётчика.
func sessionKey(r *http.Request) string {
	if tok, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer "); ok && tok != "" {
		return hashKey(tok)
	}
	for _, name := range []string{SessionCookie, AdminCookie} {
		if c, err := r.Cookie(name); err == nil && c.Value != "" {
			return hashKey(c.Value)
		}
	}
	return "ip:" + guard.Host(r.RemoteAddr)
}

func hashKey(tok string) string {
	sum := sha256.Sum256([]byte(tok))
	return "s:" + hex.EncodeToString(sum[:8])
}

// checkOrigin защищает от запроса с чужого сайта. Браузер сам прикладывает cookie, поэтому изменяющий запрос
// с cookie обязан нести X-Worker: 1 и Content-Type: application/json. Чужая форма не может поставить ни то, ни другое
// без разрешения CORS. Запрос с токеном в Authorization браузер сам не добавит, ему это не нужно.
func checkOrigin(r *http.Request) error {
	switch r.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return nil
	}
	if r.Header.Get("Authorization") != "" || len(r.Cookies()) == 0 {
		return nil
	}
	mt, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if r.Header.Get("X-Worker") != "1" || mt != "application/json" {
		return core.ErrForbidden
	}
	return nil
}
