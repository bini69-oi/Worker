package api

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestRateLimitPerSession(t *testing.T) {
	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	s := New(Deps{Text: fakeTexts, Now: func() time.Time { return now },
		Log: slog.New(slog.NewTextHandler(io.Discard, nil))})
	s.Handle("GET /api/x", func(w http.ResponseWriter, r *http.Request) (any, error) { return nil, nil })

	call := func(token string) int {
		req := httptest.NewRequest("GET", "/api/x", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)
		return rec.Code
	}
	ok, limited := 0, 0
	for range 50 {
		switch call("aaa") {
		case 200:
			ok++
		case 429:
			limited++
		}
	}
	if ok != 10 || limited != 40 {
		t.Fatalf("прошло %d, отклонено %d, ждали 10 и 40", ok, limited)
	}
	if call("bbb") != 200 {
		t.Errorf("другая сессия не должна страдать от чужого потока")
	}
	now = now.Add(1100 * time.Millisecond)
	if call("aaa") != 200 {
		t.Errorf("после секунды сессия снова принимается")
	}
}

func TestRateLimitBody(t *testing.T) {
	s := newTestServer(t)
	s.Handle("GET /api/x", func(w http.ResponseWriter, r *http.Request) (any, error) { return nil, nil })
	var last *httptest.ResponseRecorder
	for range 11 {
		last = do(s, "GET", "/api/x", "")
	}
	if last.Code != 429 || !strings.Contains(last.Body.String(), "E5007") {
		t.Errorf("%d %s", last.Code, last.Body)
	}
}

func TestMutatingWithCookieNeedsHeader(t *testing.T) {
	cases := []struct {
		name   string
		method string
		header map[string]string
		cookie bool
		want   int
	}{
		{"форма с чужого сайта", "POST", map[string]string{"Content-Type": "application/x-www-form-urlencoded"}, true, 403},
		{"json без X-Worker", "POST", map[string]string{"Content-Type": "application/json"}, true, 403},
		{"X-Worker без json", "POST", map[string]string{"X-Worker": "1"}, true, 403},
		{"X-Worker другой", "POST", map[string]string{"X-Worker": "0", "Content-Type": "application/json"}, true, 403},
		{"верный запрос", "POST", map[string]string{"X-Worker": "1", "Content-Type": "application/json"}, true, 200},
		{"json с кодировкой", "POST", map[string]string{"X-Worker": "1", "Content-Type": "application/json; charset=utf-8"}, true, 200},
		{"DELETE без заголовка", "DELETE", nil, true, 403},
		{"токен без заголовка", "POST", map[string]string{"Authorization": "Bearer t"}, false, 200},
		{"токен и cookie", "POST", map[string]string{"Authorization": "Bearer t"}, true, 200},
		{"без cookie и токена", "POST", nil, false, 200},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := newTestServer(t)
			ran := false
			h := func(w http.ResponseWriter, r *http.Request) (any, error) { ran = true; return nil, nil }
			s.Handle("POST /api/x", h)
			s.Handle("DELETE /api/x", h)
			req := httptest.NewRequest(c.method, "/api/x", nil)
			for k, v := range c.header {
				req.Header.Set(k, v)
			}
			if c.cookie {
				req.AddCookie(&http.Cookie{Name: "worker_session", Value: "t"})
			}
			rec := httptest.NewRecorder()
			s.ServeHTTP(rec, req)
			if rec.Code != c.want {
				t.Fatalf("статус %d, ждали %d", rec.Code, c.want)
			}
			if c.want == 403 && ran {
				t.Errorf("обработчик выполнился, хотя запрос чужой")
			}
		})
	}
}

func TestReadingWithCookieNeedsNoHeader(t *testing.T) {
	s := newTestServer(t)
	s.Handle("GET /api/x", func(w http.ResponseWriter, r *http.Request) (any, error) { return nil, nil })
	req := httptest.NewRequest("GET", "/api/x", nil)
	req.AddCookie(&http.Cookie{Name: "worker_session", Value: "t"})
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Errorf("чтение с cookie: %d", rec.Code)
	}
}
