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
	tgIP      *guard.Limiter // входы Mini App с одного IP
	tgID      *guard.Limiter // входы Mini App одного Telegram ID: подпись initData живёт час и годится на повтор
}

// Предел запусков входа: 10 за 10 минут с одного IP. Предел неудачных проверок токена: 30 в минуту.
const (
	loginStarts   = 10
	loginWindow   = 10 * time.Minute
	authFailLimit = 30
	// Входы Mini App: с одного адреса 60 в минуту (за адресом бывает целая сеть), одному Telegram ID 10 за 10 минут.
	tgPerIP  = 60
	tgPerID  = 10
	tgWindow = 10 * time.Minute
)

// RegisterApp заводит адреса /api/app/* на сервере.
func RegisterApp(s *Server, d AppDeps) *App {
	a := &App{AppDeps: d, srv: s,
		loginRate: &guard.Limiter{Max: loginStarts, Window: loginWindow, Now: s.deps.Now},
		authFail:  &guard.Limiter{Max: authFailLimit, Window: time.Minute, Now: s.deps.Now},
		tgIP:      &guard.Limiter{Max: tgPerIP, Window: time.Minute, Now: s.deps.Now},
		tgID:      &guard.Limiter{Max: tgPerID, Window: tgWindow, Now: s.deps.Now}}
	s.deps.SessionID = a.sessionID
	a.sessionRoutes()
	a.loginRoutes()
	a.meRoutes()
	a.buyRoutes()
	a.payRoutes()
	a.accountRoutes()
	a.connectRoutes()
	a.planRoutes()
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

// tokenOf берёт токен запроса. Если есть заголовок Authorization, берётся только он, и cookie не смотрится вовсе:
// заголовок браузер сам не добавит, а смешивать два источника значит дать подделке второй шанс.
func tokenOf(r *http.Request) (token string, cookie bool) {
	if h := r.Header.Get("Authorization"); h != "" {
		tok, _ := strings.CutPrefix(h, "Bearer ")
		if tok == h {
			return "", false // не Bearer: токена нет, cookie в расчёт не идёт
		}
		return tok, false
	}
	if c, err := r.Cookie(SessionCookie); err == nil {
		return c.Value, true
	}
	return "", false
}

// lookup проверяет сессию запроса один раз: и счётчик частоты, и обработчик получают один ответ. Если с этого IP
// накопилось слишком много неверных токенов, в базу не ходим вовсе. Перебор токенов и мусор в заголовке дальше предела не идут.
func (a *App) lookup(r *http.Request) (Auth, error) {
	m := memoOf(r.Context())
	if m == nil {
		return a.check(r)
	}
	m.once.Do(func() { m.auth, m.err = a.check(r) })
	return m.auth, m.err
}

func (a *App) check(r *http.Request) (Auth, error) {
	au := Auth{}
	au.Token, au.cookie = tokenOf(r)
	if au.Token == "" {
		return au, core.ErrSessionGone
	}
	ip := guard.Key(r.RemoteAddr)
	if a.authFail.Over(ip) {
		return au, core.ErrTooManyTries
	}
	var err error
	au.User, au.Session, err = a.Core.SessionUser(r.Context(), au.Token)
	if errors.Is(err, core.ErrSessionGone) {
		a.authFail.Allow(ip)
	}
	return au, err
}

// sessionID отдаёт номер проверенной сессии для счётчика частоты.
func (a *App) sessionID(r *http.Request) string {
	if au, err := a.lookup(r); err == nil {
		return au.Session.ID
	}
	return ""
}

// authenticate отдаёт абонента запроса. Закрытая сессия в cookie стирает cookie.
func (a *App) authenticate(w http.ResponseWriter, r *http.Request) (Auth, error) {
	au, err := a.lookup(r)
	if errors.Is(err, core.ErrSessionGone) && au.cookie {
		http.SetCookie(w, clearedCookie())
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
