package api

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/bini69-oi/worker/core"
	"github.com/bini69-oi/worker/guard"
)

// AdminCookie это имя cookie сессии админки. Оно не совпадает с cookie кабинета (SessionCookie),
// и живёт на своём пути: сессия одного сайта не открывает другой.
const AdminCookie = "worker_admin"

// adminPath это путь cookie админки.
const adminPath = "/api/admin"

// AdminDeps это то, что нужно API админки снаружи.
type AdminDeps struct {
	Core *core.Core
	// Bot отдаёт имя бота без @ для ссылок вида t.me/<бот>?start=promo_<код>. Пока Telegram не ответил, пусто.
	Bot func() string
}

type adminAPI struct {
	s *Server
	d AdminDeps
}

// RegisterAdmin заводит адреса /api/admin/*: вход без сессии и таблицу прав со всем остальным.
func RegisterAdmin(s *Server, d AdminDeps) {
	a := &adminAPI{s: s, d: d}
	s.Handle("POST /api/admin/login", a.login)
	s.Handle("GET /api/admin/login/{id}", a.loginPoll)
	for _, rt := range a.routes() {
		s.Handle(rt.Pattern, a.guarded(rt))
	}
}

// adminCtx это то, что обработчик получает про вошедшего админа.
type adminCtx struct {
	Admin   core.Admin
	Session core.Session
}

type adminHandler func(w http.ResponseWriter, r *http.Request, ad adminCtx) (any, error)

// auth проверяет сессию админки на каждом запросе. Только cookie админки: сессия кабинета сюда не подходит.
// Адрес запроса идёт в контекст, чтобы журнал знал, откуда пришло действие.
func (a *adminAPI) auth(h adminHandler) Handler {
	return func(w http.ResponseWriter, r *http.Request) (any, error) {
		c, err := r.Cookie(AdminCookie)
		if err != nil || c.Value == "" {
			return nil, core.ErrSessionGone
		}
		info := clientInfo(r)
		adm, sess, err := a.d.Core.SessionAdmin(r.Context(), c.Value, info)
		if err != nil {
			return nil, err
		}
		r = r.WithContext(core.WithIP(r.Context(), info.IP))
		return h(w, r, adminCtx{Admin: adm, Session: sess})
	}
}

// AdminMe это вошедший админ и его права: сайт по ним прячет разделы.
type AdminMe struct {
	TgID  int64    `json:"tg_id"`
	Owner bool     `json:"owner"`
	Perms []string `json:"perms"`
}

func (a *adminAPI) me(_ http.ResponseWriter, _ *http.Request, ad adminCtx) (any, error) {
	perms := make([]string, 0, len(ad.Admin.Perms))
	for _, p := range ad.Admin.Perms {
		perms = append(perms, string(p))
	}
	return AdminMe{TgID: ad.Admin.TgID, Owner: ad.Admin.Owner, Perms: perms}, nil
}

// clientInfo это откуда пришёл запрос: адрес соединения (или своего прокси, его ставит guard.RealIP) и браузер.
func clientInfo(r *http.Request) core.LoginInfo {
	return core.LoginInfo{IP: guard.Host(r.RemoteAddr), Agent: r.UserAgent()}
}

// AdminLoginStarted это ответ на начало входа: номер шага для опроса и код, который админ выбирает в Telegram.
type AdminLoginStarted struct {
	ID   string `json:"id"`
	Code string `json:"code"`
}

// AdminLoginState это ответ опроса: pending, denied, expired или ok (тогда cookie поставлена).
type AdminLoginState struct {
	Status string `json:"status"`
}

func (a *adminAPI) login(w http.ResponseWriter, r *http.Request) (any, error) {
	var in struct {
		TgID     int64  `json:"tg_id"`
		Password string `json:"password"`
	}
	if err := Decode(w, r, &in); err != nil {
		return nil, err
	}
	step, err := a.d.Core.LoginStart(r.Context(), in.TgID, in.Password, clientInfo(r))
	if err != nil {
		return nil, err
	}
	return AdminLoginStarted{ID: strconv.FormatInt(step.ConfirmID, 10) + "." + step.Nonce, Code: step.Code}, nil
}

func (a *adminAPI) loginPoll(w http.ResponseWriter, r *http.Request) (any, error) {
	num, nonce, ok := strings.Cut(r.PathValue("id"), ".")
	id, err := strconv.ParseInt(num, 10, 64)
	if !ok || err != nil || nonce == "" {
		return nil, core.ErrNotConfirmed
	}
	state, token, sess, err := a.d.Core.LoginPoll(r.Context(), id, nonce, clientInfo(r))
	if err != nil {
		return nil, err
	}
	if state == core.LoginOK {
		http.SetCookie(w, &http.Cookie{Name: AdminCookie, Value: token, Path: adminPath, Expires: sess.ExpiresAt,
			HttpOnly: true, Secure: true, SameSite: http.SameSiteStrictMode})
	}
	return AdminLoginState{Status: state}, nil
}

// logout закрывает текущую сессию и сбрасывает cookie.
func (a *adminAPI) logout(w http.ResponseWriter, r *http.Request, ad adminCtx) (any, error) {
	if err := a.d.Core.CloseSession(r.Context(), ad.Admin.TgID, ad.Session.ID); err != nil {
		return nil, err
	}
	http.SetCookie(w, &http.Cookie{Name: AdminCookie, Path: adminPath, MaxAge: -1,
		HttpOnly: true, Secure: true, SameSite: http.SameSiteStrictMode})
	return nil, nil
}
