package api

import (
	"net/http"
	"time"

	"github.com/bini69-oi/worker/core"
	"github.com/bini69-oi/worker/guard"
)

// webSessionLife это срок cookie сессии браузера, он равен сроку сессии в ядре.
const webSessionLife = 30 * 24 * time.Hour

func (a *App) loginRoutes() {
	a.srv.Handle("POST /api/app/login", a.loginStart)
	a.srv.Handle("GET /api/app/login/{id}", a.loginPoll)
}

// loginStart начинает вход в браузере: не больше 10 запусков за 10 минут с одного IP.
func (a *App) loginStart(w http.ResponseWriter, r *http.Request) (any, error) {
	var in EmptyRequest
	if err := Decode(w, r, &in); err != nil {
		return nil, err
	}
	if !a.loginRate.Allow(guard.Key(r.RemoteAddr)) {
		return nil, core.ErrTooManyTries
	}
	st, err := a.Core.WebLoginStart(r.Context(), info(r))
	if err != nil {
		return nil, err
	}
	name := a.BotName()
	if name == "" {
		a.srv.deps.Log.Error("вход в браузере: имя бота ещё не известно")
		return nil, core.ErrInternal
	}
	return LoginStartResponse{ID: st.ID, Code: st.Code, Link: "https://t.me/" + name + "?start=web_" + st.Nonce, ExpiresAt: st.ExpiresAt}, nil
}

// loginPoll это опрос браузера. Подтверждённый вход ставит cookie сессии, токен в тело не попадает.
func (a *App) loginPoll(w http.ResponseWriter, r *http.Request) (any, error) {
	res, err := a.Core.WebLoginPoll(r.Context(), r.PathValue("id"), info(r))
	if err != nil {
		return nil, err
	}
	if res.State == core.LoginConfirmed {
		http.SetCookie(w, sessionCookie(res.Token, webSessionLife))
	}
	return LoginPollResponse{State: res.State}, nil
}
