package api

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bini69-oi/worker/core"
	"github.com/bini69-oi/worker/core/coretest"
	"github.com/bini69-oi/worker/pay/manual"
	"github.com/bini69-oi/worker/pay/stars"
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
	t      *testing.T
	s      *Server
	c      *core.Core
	st     *coretest.Store
	clock  *coretest.Clock
	cfg    *settings.Settings
	panel  *coretest.Panel
	notes  *coretest.Notifier
	stars  *stars.Gateway
	sender *coretest.Sender
	owner  string // cookie владельца
}

func newAdminEnv(t *testing.T) *adminEnv {
	t.Helper()
	e := &adminEnv{t: t, st: coretest.NewStore(), clock: coretest.NewClock(t0), cfg: coretest.NewConfig(t),
		panel: coretest.NewPanel(), notes: &coretest.Notifier{}}
	for k, v := range map[string]string{"manual.enabled": "true", "manual.details": "Карта 0000", "stars.enabled": "true"} {
		if err := e.cfg.Set(k, v); err != nil {
			t.Fatal(err)
		}
	}
	e.stars = stars.New(e.cfg)
	e.sender = &coretest.Sender{}
	e.c = core.New(core.Deps{Store: e.st, Clock: e.clock, Config: e.cfg, Panel: e.panel, Notifier: e.notes, OwnerID: adminOwner, Sender: e.sender,
		Gateways: []core.Gateway{manual.New(e.cfg), e.stars}})
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	// Часы лимита запросов идут вперёд сами: тесты шлют много запросов подряд, а часы ядра стоят на месте.
	var tick atomic.Int64
	e.s = New(Deps{Text: fakeTexts, Log: log, Now: func() time.Time { return t0.Add(time.Duration(tick.Add(1)) * time.Second) }})
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

// subscriber заводит абонента с платной подпиской на 30 дней и одним устройством в панели.
func (e *adminEnv) subscriber(tg int64, name string) core.User {
	e.t.Helper()
	ctx := context.Background()
	u, err := e.c.Register(ctx, tg, name, "ru")
	if err != nil {
		e.t.Fatal(err)
	}
	tr, err := e.c.SaveTariff(ctx, adminOwner, core.Tariff{Name: "Месяц", Active: true, Squads: []string{"a"},
		Periods: []core.Period{{Days: 30, Price: 20000}}})
	if err != nil {
		e.t.Fatal(err)
	}
	if _, err := e.c.AdminAdjust(ctx, adminOwner, u.ID, 50000, "тест"); err != nil {
		e.t.Fatal(err)
	}
	if _, err := e.c.Buy(ctx, u.ID, tr.ID, 30); err != nil {
		e.t.Fatal(err)
	}
	if _, err := e.c.ProcessDue(ctx); err != nil {
		e.t.Fatal(err)
	}
	sub, err := e.c.Sub(ctx, u.ID)
	if err != nil {
		e.t.Fatal(err)
	}
	e.panel.AddDevice(sub.PanelUUID, "hw-1")
	return u
}

var contextBG = context.Background()

func httpReq(e *adminEnv, method, target, body, cookie string) *http.Request {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	req.RemoteAddr = adminIP
	req.Header.Set("Cookie", cookie)
	return req
}

func serve(e *adminEnv, req *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	e.s.ServeHTTP(rec, req)
	return rec
}
