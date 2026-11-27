package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/bini69-oi/worker/core"
	"github.com/bini69-oi/worker/guard"
)

// AppDeps это то, что нужно кабинету снаружи.
type AppDeps struct {
	Core   *core.Core
	Config core.Config
	// BotToken и BotName отдают токен и имя бота сейчас: оба могут смениться без перезапуска.
	BotToken func() string
	BotName  func() string
	// PublicURL это адрес Worker без слеша на конце: от него считаются переходы /dl.
	PublicURL string
	// DLKey подписывает переходы /dl.
	DLKey []byte
	// Texts отдаёт тексты кабинета и ошибок на языке lang: по префиксам ключей.
	Texts func(lang string, prefixes ...string) map[string]string
	// StarsInvoice выставляет счёт Stars и отдаёт ссылку для WebApp.openInvoice.
	StarsInvoice func(ctx context.Context, title, desc, payload string, stars int64) (string, error)
}

// App это обработчики кабинета: общие зависимости и пределы.
type App struct {
	AppDeps
	srv       *Server
	loginRate *guard.Limiter // запуски входа в браузере с одного IP
	authFail  *guard.Limiter // неудачные проверки токена с одного IP: перебор токенов
}

// Предел запусков входа: 10 за 10 минут с одного IP. Предел неудачных проверок токена: 30 в минуту.
const (
	loginStarts   = 10
	loginWindow   = 10 * time.Minute
	authFailLimit = 30
)

// RegisterApp заводит адреса /api/app/* на сервере.
func RegisterApp(s *Server, d AppDeps) *App {
	a := &App{AppDeps: d, srv: s,
		loginRate: &guard.Limiter{Max: loginStarts, Window: loginWindow, Now: s.deps.Now},
		authFail:  &guard.Limiter{Max: authFailLimit, Window: time.Minute, Now: s.deps.Now}}
	a.sessionRoutes()
	a.loginRoutes()
	a.meRoutes()
	a.buyRoutes()
	a.payRoutes()
	return a
}

// Auth это абонент и сессия, которые API нашёл по токену запроса.
type Auth struct {
	User    core.User
	Session core.UserSession
	Token   string
	cookie  bool // токен пришёл в cookie, а не заголовком
}

// AuthHandler это обработчик адреса, на который абонент вошёл.
type AuthHandler func(w http.ResponseWriter, r *http.Request, a Auth) (any, error)

// langError несёт язык абонента вместе с ошибкой: на нём пишется текст ответа.
type langError struct {
	err  error
	lang string
}

func (e *langError) Error() string { return e.err.Error() }
func (e *langError) Unwrap() error { return e.err }

// handleOpen это адрес, который работает и во время техработ: главная, тексты и выход.
func (a *App) handleOpen(pattern string, h AuthHandler) { a.srv.Handle(pattern, a.auth(h, false)) }

func (a *App) auth(h AuthHandler, gated bool) Handler {
	return func(w http.ResponseWriter, r *http.Request) (any, error) {
		au, err := a.authenticate(w, r)
		if err != nil {
			return nil, err
		}
		fail := func(err error) error { return &langError{err: err, lang: au.User.Lang} }
		if gated {
			if err := a.Core.Serving(r.Context(), au.User.ID); err != nil {
				return nil, fail(err)
			}
		}
		res, err := h(w, r, au)
		if err != nil {
			return nil, fail(err)
		}
		return res, nil
	}
}

// authenticate ищет токен (заголовок Authorization, потом cookie) и проверяет его у ядра.
// Закрытая сессия в cookie стирает cookie. Перебор токенов с одного адреса упирается в предел.
func (a *App) authenticate(w http.ResponseWriter, r *http.Request) (Auth, error) {
	au := Auth{}
	if tok, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer "); ok {
		au.Token = tok
	} else if c, err := r.Cookie(SessionCookie); err == nil {
		au.Token, au.cookie = c.Value, true
	}
	var err error
	au.User, au.Session, err = a.Core.SessionUser(r.Context(), au.Token)
	if err == nil {
		return au, nil
	}
	if errors.Is(err, core.ErrSessionGone) {
		if au.cookie {
			http.SetCookie(w, clearedCookie())
		}
		if !a.authFail.Allow(guard.Host(r.RemoteAddr)) {
			return au, core.ErrTooManyTries
		}
	}
	return au, err
}

// info это откуда пришёл запрос: IP из соединения (за своим прокси guard.RealIP уже подставил настоящий) и браузер.
func info(r *http.Request) core.LoginInfo {
	return core.LoginInfo{IP: guard.Host(r.RemoteAddr), Agent: r.UserAgent()}
}

// sessionCookie это cookie сессии браузера: только для запросов к API кабинета, недоступна скриптам,
// не уходит на чужие сайты.
func sessionCookie(token string, maxAge time.Duration) *http.Cookie {
	return &http.Cookie{Name: SessionCookie, Value: token, Path: "/api/app", MaxAge: int(maxAge.Seconds()),
		HttpOnly: true, Secure: true, SameSite: http.SameSiteStrictMode}
}

func clearedCookie() *http.Cookie {
	c := sessionCookie("", 0)
	c.MaxAge = -1
	return c
}
