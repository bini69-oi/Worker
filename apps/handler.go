package apps

import (
	"context"
	"errors"
	"html"
	"net/http"
	"time"

	"github.com/bini69-oi/worker/core"
)

// Subs это то, что переходу нужно от ядра: текущая подписка абонента.
type Subs interface {
	Sub(ctx context.Context, userID int64) (core.Subscription, error)
}

// Handler отвечает на GET /dl/{app}/{token}: проверяет токен, берёт текущую ссылку подписки
// и отправляет браузер в приложение. Ссылка берётся каждый раз из ядра, поэтому после перевыпуска
// старая кнопка ведёт на новую ссылку. text отдаёт текст страницы по ключу (dl.expired, dl.pending, dl.gone).
func Handler(subs Subs, key []byte, now func() time.Time, text func(key string) string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		app, ok := Find(r.PathValue("app"))
		if !ok {
			http.NotFound(w, r)
			return
		}
		userID, err := Parse(key, r.PathValue("token"), now())
		if errors.Is(err, ErrExpired) {
			page(w, http.StatusGone, text("dl.expired"))
			return
		}
		if err != nil {
			http.NotFound(w, r)
			return
		}
		sub, err := subs.Sub(r.Context(), userID)
		if errors.Is(err, core.ErrNotFound) {
			page(w, http.StatusNotFound, text("dl.gone"))
			return
		}
		if err != nil {
			http.Error(w, "error", http.StatusInternalServerError)
			return
		}
		if sub.SubURL == "" {
			page(w, http.StatusOK, text("dl.pending"))
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		http.Redirect(w, r, app.Open(sub.SubURL), http.StatusFound)
	})
}

// page отдаёт короткую страницу с одной фразой.
func page(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Security-Policy", "default-src 'none'")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(`<!doctype html><meta charset="utf-8"><meta name="viewport" content="width=device-width">` +
		`<p style="font:18px system-ui;margin:2em">` + html.EscapeString(msg) + `</p>`))
}
