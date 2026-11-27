package main

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/bini69-oi/worker/api"
	"github.com/bini69-oi/worker/core"
	"github.com/bini69-oi/worker/texts"
)

// mountAdmin подключает API админки на /api/admin/. Тексты ошибок берутся из редактора текстов, поэтому
// правка текста ошибки действует и здесь. Свой сервер у админки, отдельный от кабинета: у них разные сессии.
func mountAdmin(mux *http.ServeMux, c *core.Core, tx *texts.Texts, t *telegram, log *slog.Logger) {
	srv := api.New(api.Deps{
		Text: func(key, lang string) string { return tx.Render(key, lang, nil) },
		Log:  log,
		Now:  time.Now,
	})
	api.RegisterAdmin(srv, api.AdminDeps{Core: c, Bot: t.botName})
	mux.Handle("/api/admin/", srv)
}
