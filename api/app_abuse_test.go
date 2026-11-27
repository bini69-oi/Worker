package api

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func (e *appEnv) count(table string) int {
	e.t.Helper()
	var n int
	if err := e.db.W.QueryRow("SELECT count(*) FROM " + table).Scan(&n); err != nil {
		e.t.Fatal(err)
	}
	return n
}

// Случайный Bearer не обходит предел частоты, а одни и те же initData не плодят сессии без предела.
func TestSessionFloodRepelled(t *testing.T) {
	e := newAppEnv(t)
	body := tgBody(initData(77, "ru", e.clock.Now().Add(-time.Minute)))
	ok := 0
	for i := range 500 {
		if e.req("POST", "/api/app/session/tg", body, "Authorization", fmt.Sprintf("Bearer junk%d", i)).Code == 200 {
			ok++
		}
	}
	if ok > tgPerID {
		t.Fatalf("выдано %d сессий одному Telegram ID, предел %d", ok, tgPerID)
	}
	if n := e.count("user_sessions"); n > tgPerID {
		t.Fatalf("строк сессий %d", n)
	}
}

// Мусорные токены на обычных адресах упираются в предел своего IP, а потом в предел неудачных проверок.
func TestJunkBearerRepelled(t *testing.T) {
	e := newAppEnv(t)
	ok, limited := 0, 0
	for i := range 300 {
		switch e.req("GET", "/api/app/me", "", "Authorization", fmt.Sprintf("Bearer junk%d", i)).Code {
		case 401:
			ok++
		case 429:
			limited++
		}
	}
	if ok > 10 || limited < 290 {
		t.Fatalf("401 %d, 429 %d из 300: случайные токены обходят предел", ok, limited)
	}
	// С другого IP честный абонент входит как обычно.
	body := tgBody(initData(78, "ru", e.clock.Now()))
	r := httptest.NewRequest("POST", "/api/app/session/tg", strings.NewReader(body))
	r.RemoteAddr = "198.51.100.7:1"
	rec := httptest.NewRecorder()
	e.s.ServeHTTP(rec, r)
	if rec.Code != 200 {
		t.Fatalf("честный вход с другого IP: %d", rec.Code)
	}
}

// Неверные токены с одного IP закрывают поход в базу: после предела проверка сессии даже не начинается.
func TestAuthFailBlocksBeforeDB(t *testing.T) {
	e := newAppEnv(t)
	good := e.login(5)
	for i := range 200 {
		e.clock.Add(150 * time.Millisecond) // общий предел в секунду не мешает
		e.req("GET", "/api/app/me", "", "Authorization", fmt.Sprintf("Bearer bad%d", i))
	}
	e.clock.Add(150 * time.Millisecond)
	if rec := e.as(good, "GET", "/api/app/me", ""); rec.Code != 429 {
		t.Fatalf("с адреса, который перебирает токены, даже верный токен не проверяется: %d", rec.Code)
	}
	e.clock.Add(2 * time.Minute)
	if rec := e.as(good, "GET", "/api/app/me", ""); rec.Code != 200 {
		t.Fatalf("после окна: %d", rec.Code)
	}
}

// Запросы входа с разных адресов одной сети IPv6 /64 считаются одним адресом и не растят таблицу.
func TestLoginIPv6NetworkFolded(t *testing.T) {
	e := newAppEnv(t)
	for i := range 300 {
		r := httptest.NewRequest("POST", "/api/app/login", strings.NewReader("{}"))
		r.RemoteAddr = fmt.Sprintf("[2001:db8:1:2::%x]:1", i+1)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Authorization", fmt.Sprintf("Bearer x%d", i))
		e.clock.Add(time.Second)
		e.s.ServeHTTP(httptest.NewRecorder(), r)
	}
	if n := e.count("user_logins"); n > loginStarts {
		t.Fatalf("строк user_logins %d, предел %d на сеть", n, loginStarts)
	}
	if rec := e.req("POST", "/api/app/login", "{}"); rec.Code != 200 {
		t.Fatalf("честный абонент с IPv4: %d", rec.Code)
	}
}

// Проверенная сессия считается отдельно: поток одного абонента не мешает другому за тем же адресом.
func TestSessionRateIsPerVerifiedSession(t *testing.T) {
	e := newAppEnv(t)
	a, b := e.login(1), e.login(2)
	e.clock.Add(time.Second)
	limited := 0
	for range 30 {
		if e.as(a, "GET", "/api/app/me", "").Code == 429 {
			limited++
		}
	}
	if limited < 15 {
		t.Fatalf("поток одной сессии не ограничен: %d", limited)
	}
	if rec := e.as(b, "GET", "/api/app/me", ""); rec.Code != 200 {
		t.Fatalf("соседняя сессия страдает от чужого потока: %d", rec.Code)
	}
}

// Токен в Authorization главнее cookie, а cookie при этом не смотрится: чужой Bearer не переключает на cookie.
func TestBearerOverridesCookie(t *testing.T) {
	e := newAppEnv(t)
	good := e.login(9)
	rec := e.req("GET", "/api/app/me", "", "Authorization", "Bearer wrong", "Cookie", SessionCookie+"="+good)
	if rec.Code != 401 {
		t.Fatalf("неверный Bearer с верной cookie: %d, ждали 401", rec.Code)
	}
	rec = e.req("GET", "/api/app/me", "", "Authorization", "Basic abc", "Cookie", SessionCookie+"="+good)
	if rec.Code != 401 {
		t.Fatalf("не Bearer с верной cookie: %d", rec.Code)
	}
}

// Любое изменяющее действие с cookie требует X-Worker, даже если пришёл заголовок Authorization.
func TestCookieNeedsXWorkerEvenWithAuthorization(t *testing.T) {
	e := newAppEnv(t)
	tok := e.login(9)
	for _, auth := range []string{"Bearer " + tok, "Basic abc", "x"} {
		rec := e.req("POST", "/api/app/promo", `{"code":"ABCDEF"}`, "Authorization", auth, "Cookie", SessionCookie+"=z")
		if rec.Code != 403 {
			t.Errorf("Authorization %q с cookie без X-Worker: %d, ждали 403", auth, rec.Code)
		}
	}
}

// В журнал идёт шаблон маршрута, а не путь: в пути входа в браузере лежит секрет запроса.
func TestLogHasRouteNotPath(t *testing.T) {
	var buf strings.Builder
	s := New(Deps{Text: fakeTexts, Log: slog.New(slog.NewTextHandler(&buf, nil))})
	s.Handle("GET /api/app/login/{id}", func(w http.ResponseWriter, r *http.Request) (any, error) { return nil, errors.New("сбой") })
	s.Handle("GET /api/app/boom/{id}", func(w http.ResponseWriter, r *http.Request) (any, error) { panic("x") })
	for _, p := range []string{"/api/app/login/SECRET-LOGIN-ID", "/api/app/boom/SECRET-LOGIN-ID"} {
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, httptest.NewRequest("GET", p, nil))
		if rec.Code != 500 {
			t.Fatalf("%s: %d", p, rec.Code)
		}
	}
	if strings.Contains(buf.String(), "SECRET-LOGIN-ID") {
		t.Fatalf("секрет попал в журнал: %s", buf.String())
	}
	if !strings.Contains(buf.String(), "/api/app/login/{id}") {
		t.Fatalf("в журнале нет шаблона маршрута: %s", buf.String())
	}
}
