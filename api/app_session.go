package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/bini69-oi/worker/core"
	"github.com/bini69-oi/worker/guard"
	"github.com/bini69-oi/worker/tgsign"
)

// initDataMaxAge это срок, в который принимаются подписанные данные Mini App.
const initDataMaxAge = time.Hour

func (a *App) sessionRoutes() {
	s := a.srv
	s.Handle("POST /api/app/session/tg", a.sessionTg)
	a.handleOpen("POST /api/app/logout", a.logout)
	a.handleOpen("POST /api/app/logout-all", a.logoutAll)
}

// sessionTg меняет подписанные данные Mini App на токен сессии. Подпись и свежесть проверяет tgsign,
// всё остальное ядро: оно же регистрирует нового абонента и отказывает забаненному.
func (a *App) sessionTg(w http.ResponseWriter, r *http.Request) (any, error) {
	var in SessionTgRequest
	if err := Decode(w, r, &in); err != nil {
		return nil, err
	}
	ip := guard.Key(r.RemoteAddr)
	if !a.tgIP.Allow(ip) {
		return nil, core.ErrTooManyTries
	}
	data, err := tgsign.VerifyInitData(in.InitData, a.BotToken(), a.now(), initDataMaxAge)
	if err != nil {
		if !a.authFail.Allow(ip) {
			return nil, core.ErrTooManyTries
		}
		return nil, core.ErrBadInitData
	}
	// Подписанные данные годятся час: без предела по ID одни и те же initData плодили бы сессии и записи в базе.
	if !a.tgID.Allow(itoa(data.User.ID)) {
		return nil, core.ErrTooManyTries
	}
	token, u, err := a.Core.TgLogin(r.Context(), data.User.ID, data.User.Username, data.User.LanguageCode, data.StartParam, info(r))
	if err != nil {
		return nil, &langError{err: err, lang: u.Lang}
	}
	return SessionResponse{Token: token, Lang: u.Lang, ExpiresAt: a.now().Add(24 * time.Hour)}, nil
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

func (a *App) now() time.Time {
	if a.srv.deps.Now != nil {
		return a.srv.deps.Now()
	}
	return time.Now()
}

func (a *App) logout(w http.ResponseWriter, r *http.Request, au Auth) (any, error) {
	if err := a.Core.Logout(r.Context(), au.Token); err != nil {
		return nil, err
	}
	if au.cookie {
		http.SetCookie(w, clearedCookie())
	}
	return nil, nil
}

func (a *App) logoutAll(w http.ResponseWriter, r *http.Request, au Auth) (any, error) {
	if err := a.Core.LogoutAll(r.Context(), au.User.ID); err != nil {
		return nil, err
	}
	if au.cookie {
		http.SetCookie(w, clearedCookie())
	}
	return nil, nil
}
