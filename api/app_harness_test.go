package api

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/bini69-oi/worker/core"
	"github.com/bini69-oi/worker/core/coretest"
	"github.com/bini69-oi/worker/pay/gatewaytest"
	"github.com/bini69-oi/worker/pay/manual"
	"github.com/bini69-oi/worker/pay/stars"
	"github.com/bini69-oi/worker/settings"
	"github.com/bini69-oi/worker/store"
)

const (
	testBotToken = "123456:TEST-TOKEN"
	testOwner    = 1
)

var t0 = time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)

// appEnv это настоящее ядро на SQLite во временном файле, фейковая панель и ручная касса, а перед ними API кабинета.
type appEnv struct {
	t     *testing.T
	s     *Server
	c     *core.Core
	cfg   *settings.Settings
	clock *coretest.Clock
	panel *coretest.Panel
	db    *store.DB
	fake  *gatewaytest.Fake
	// invoices это счета Stars, которые API выставил через Telegram.
	invoices []starsInvoice
}

type starsInvoice struct {
	Title, Desc, Payload string
	Stars                int64
}

func newAppEnv(t *testing.T) *appEnv {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "w.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	e := &appEnv{t: t, clock: coretest.NewClock(t0), cfg: coretest.NewConfig(t), panel: coretest.NewPanel(), db: db}
	for k, v := range map[string]string{"manual.enabled": "true", "manual.details": "Карта 0000"} {
		if err := e.cfg.Set(k, v); err != nil {
			t.Fatal(err)
		}
	}
	e.fake = &gatewaytest.Fake{Secret: "s", Paid: map[string]bool{}}
	e.c = core.New(core.Deps{Store: store.NewCoreStore(db), Clock: e.clock, Config: e.cfg, Panel: e.panel,
		Gateways: []core.Gateway{manual.New(e.cfg), e.fake, stars.New(e.cfg)}, OwnerID: testOwner})
	e.s = New(Deps{Text: fakeTexts, Log: slog.New(slog.NewTextHandler(io.Discard, nil)), Now: e.clock.Now})
	RegisterApp(e.s, AppDeps{Core: e.c, Config: e.cfg, BotToken: func() string { return testBotToken },
		BotName: func() string { return "worker_bot" }, Texts: fakeTextMap, PublicURL: "https://w.test", StarsInvoice: e.starsInvoice, DLKey: []byte(strings.Repeat("k", 32))})
	return e
}

// initData подписывает данные Mini App токеном бота, как это делает Telegram.
func initData(tgID int64, lang string, authAt time.Time) string {
	user, _ := json.Marshal(map[string]any{"id": tgID, "first_name": "Ivan", "username": "ivan" + strconv.FormatInt(tgID, 10), "language_code": lang})
	return signInitData(map[string]string{"auth_date": strconv.FormatInt(authAt.Unix(), 10), "user": string(user), "query_id": "q"}, testBotToken)
}

func signInitData(fields map[string]string, token string) string {
	keys := make([]string, 0, len(fields))
	for k := range fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var parts []string
	for _, k := range keys {
		parts = append(parts, k+"="+fields[k])
	}
	km := hmac.New(sha256.New, []byte("WebAppData"))
	km.Write([]byte(token))
	m := hmac.New(sha256.New, km.Sum(nil))
	m.Write([]byte(strings.Join(parts, "\n")))
	v := url.Values{}
	for k, val := range fields {
		v.Set(k, val)
	}
	v.Set("hash", hex.EncodeToString(m.Sum(nil)))
	return v.Encode()
}

// req это запрос к API. Заголовки задаются парами «имя, значение».
func (e *appEnv) req(method, target, body string, headers ...string) *httptest.ResponseRecorder {
	e.t.Helper()
	r := httptest.NewRequest(method, target, strings.NewReader(body))
	r.RemoteAddr = "10.0.0.1:5555"
	r.Header.Set("User-Agent", "TestBrowser")
	if body != "" {
		r.Header.Set("Content-Type", "application/json")
	}
	for i := 0; i+1 < len(headers); i += 2 {
		r.Header.Set(headers[i], headers[i+1])
	}
	rec := httptest.NewRecorder()
	e.s.ServeHTTP(rec, r)
	return rec
}

// login входит в Mini App и отдаёт токен сессии.
func (e *appEnv) login(tgID int64) string {
	e.t.Helper()
	body, _ := json.Marshal(map[string]string{"init_data": initData(tgID, "ru", e.clock.Now().Add(-time.Minute))})
	rec := e.req("POST", "/api/app/session/tg", string(body))
	if rec.Code != 200 {
		e.t.Fatalf("вход в Mini App: %d %s", rec.Code, rec.Body)
	}
	var out struct{ Token string }
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || out.Token == "" {
		e.t.Fatalf("в ответе входа нет токена: %s", rec.Body)
	}
	return out.Token
}

func decode(t *testing.T, rec *httptest.ResponseRecorder, dst any) {
	t.Helper()
	if err := json.Unmarshal(rec.Body.Bytes(), dst); err != nil {
		t.Fatalf("ответ не JSON: %v\n%s", err, rec.Body)
	}
}

func cookieNamed(rec *httptest.ResponseRecorder, name string) *http.Cookie {
	for _, c := range rec.Result().Cookies() {
		if c.Name == name {
			return c
		}
	}
	return nil
}

func strReplace(s, old, repl string) string { return strings.Replace(s, old, repl, 1) }

// as делает запрос с токеном сессии в заголовке.
func (e *appEnv) as(token, method, target, body string) *httptest.ResponseRecorder {
	e.t.Helper()
	return e.req(method, target, body, "Authorization", "Bearer "+token)
}

// fakeTextMap отдаёт по два ключа на префикс со значением «язык:ключ».
func fakeTextMap(lang string, prefixes ...string) map[string]string {
	out := map[string]string{}
	for _, p := range prefixes {
		k := p + "hello"
		if p == "err." {
			k = "err.x"
		}
		out[k] = lang + ":" + k
	}
	return out
}

func (e *appEnv) starsInvoice(_ context.Context, title, desc, payload string, n int64) (string, error) {
	e.invoices = append(e.invoices, starsInvoice{title, desc, payload, n})
	return "https://t.me/$invoice-" + payload, nil
}

// enable включает кассы по имени.
func (e *appEnv) enable(names ...string) {
	e.t.Helper()
	for _, n := range names {
		if err := e.cfg.Set(n+".enabled", "true"); err != nil {
			e.t.Fatal(err)
		}
	}
}
