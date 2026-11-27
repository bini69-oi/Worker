package api

import (
	"mime"
	"net/http"

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
	if !s.limit.Allow(s.sessionKey(r)) {
		return core.ErrTooManyTries
	}
	return checkOrigin(r)
}

// sessionKey это то, по чему считается частота: номер проверенной сессии, а без неё IP (IPv6 по сети /64).
// Токен, который сервер не признал, ключа не получает: случайные токены не плодят счётчики.
func (s *Server) sessionKey(r *http.Request) string {
	if s.deps.SessionID != nil {
		if id := s.deps.SessionID(r); id != "" {
			return "s:" + id
		}
	}
	return "ip:" + guard.Key(r.RemoteAddr)
}

// checkOrigin защищает от запроса с чужого сайта. Браузер сам прикладывает cookie, поэтому изменяющий запрос
// с cookie обязан нести X-Worker: 1 и Content-Type: application/json. Чужая форма не может поставить ни то, ни другое
// без разрешения CORS.
func checkOrigin(r *http.Request) error {
	switch r.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return nil
	}
	// Cookie сессии есть: браузер приложит её к любому запросу, в том числе чужому, поэтому X-Worker обязателен
	// всегда, с заголовком Authorization или без. Нет cookie сессии: подделывать нечего.
	_, errApp := r.Cookie(SessionCookie)
	_, errAdmin := r.Cookie(AdminCookie)
	if errApp != nil && errAdmin != nil {
		return nil
	}
	mt, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if r.Header.Get("X-Worker") != "1" || mt != "application/json" {
		return core.ErrForbidden
	}
	return nil
}
