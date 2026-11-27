package api

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/bini69-oi/worker/core"
	"github.com/bini69-oi/worker/core/coretest"
	"github.com/bini69-oi/worker/settings"
)

const (
	adminOwner = 1
	ownerPass  = "correct horse battery"
	adminIP    = "203.0.113.5:4000"
)

var t0 = time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

// adminEnv это сервер админки на фейках: ядро, часы, хранилище, панель.
type adminEnv struct {
	t     *testing.T
	s     *Server
	c     *core.Core
	st    *coretest.Store
	clock *coretest.Clock
	cfg   *settings.Settings
	panel *coretest.Panel
	notes *coretest.Notifier
	owner string // cookie владельца
}

func newAdminEnv(t *testing.T) *adminEnv {
	t.Helper()
	e := &adminEnv{t: t, st: coretest.NewStore(), clock: coretest.NewClock(t0), cfg: coretest.NewConfig(t),
		panel: coretest.NewPanel(), notes: &coretest.Notifier{}}
	e.c = core.New(core.Deps{Store: e.st, Clock: e.clock, Config: e.cfg, Panel: e.panel, Notifier: e.notes, OwnerID: adminOwner})
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	e.s = New(Deps{Text: fakeTexts, Log: log, Now: e.clock.Now})
	RegisterAdmin(e.s, AdminDeps{Core: e.c, Bot: func() string { return "test_bot" }})
	if err := e.c.SetOwnerPassword(context.Background(), ownerPass); err != nil {
		t.Fatal(err)
	}
	e.owner = e.session(adminOwner, ownerPass)
	return e
}

// session проводит вход через ядро и отдаёт cookie сессии.
func (e *adminEnv) session(tg int64, pass string) string {
	e.t.Helper()
	ctx := context.Background()
	info := core.LoginInfo{IP: "203.0.113.5", Agent: "test"}
	step, err := e.c.LoginStart(ctx, tg, pass, info)
	if err != nil {
		e.t.Fatal(err)
	}
	if err := e.c.AnswerLogin(ctx, step.ConfirmID, tg, step.Code); err != nil {
		e.t.Fatal(err)
	}
	token, _, err := e.c.LoginFinish(ctx, step.ConfirmID, step.Nonce, info)
	if err != nil {
		e.t.Fatal(err)
	}
	return AdminCookie + "=" + token
}

// do шлёт запрос с cookie. Изменяющий запрос получает заголовки защиты, как с настоящей страницы.
func (e *adminEnv) do(cookie, method, target, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	req.RemoteAddr = adminIP
	req.Header.Set("User-Agent", "test")
	if cookie != "" {
		req.Header.Set("Cookie", cookie)
	}
	if method != http.MethodGet {
		req.Header.Set("X-Worker", "1")
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	e.s.ServeHTTP(rec, req)
	return rec
}

func (e *adminEnv) get(cookie, target string) *httptest.ResponseRecorder {
	return e.do(cookie, "GET", target, "")
}

func (e *adminEnv) post(cookie, target, body string) *httptest.ResponseRecorder {
	return e.do(cookie, "POST", target, body)
}

func errCode(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var b ErrorBody
	if err := json.Unmarshal(rec.Body.Bytes(), &b); err != nil {
		t.Fatalf("тело не JSON: %q", rec.Body)
	}
	return b.Code
}

// helper заводит помощника с правами (через «Да» владельца) и отдаёт его cookie.
func (e *adminEnv) helper(tg int64, perms ...core.Perm) string {
	e.t.Helper()
	ctx := context.Background()
	id, err := e.c.RequestAddHelper(ctx, adminOwner, tg, perms, "helper password 1")
	if err != nil {
		e.t.Fatal(err)
	}
	if err := e.c.AnswerConfirm(ctx, id, adminOwner, true); err != nil {
		e.t.Fatal(err)
	}
	return e.session(tg, "helper password 1")
}
