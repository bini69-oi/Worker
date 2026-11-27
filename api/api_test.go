package api

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bini69-oi/worker/core"
)

// fakeTexts отдаёт текст «<язык>:<ключ>», чтобы тест видел, что текст взят по ключу и на нужном языке.
func fakeTexts(key, lang string) string {
	if lang == "" {
		lang = "ru"
	}
	return lang + ":" + key
}

func newTestServer(t *testing.T) *Server {
	t.Helper()
	return New(Deps{Text: fakeTexts, Log: slog.New(slog.NewTextHandler(io.Discard, nil))})
}

func do(s *Server, method, target, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	return rec
}

func TestJSONResponse(t *testing.T) {
	s := newTestServer(t)
	s.Handle("GET /api/x", func(w http.ResponseWriter, r *http.Request) (any, error) {
		return map[string]int{"n": 1}, nil
	})
	rec := do(s, "GET", "/api/x", "")
	if rec.Code != 200 {
		t.Fatalf("статус %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type %q", ct)
	}
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("ответ API не должен кэшироваться")
	}
	if rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Errorf("нет nosniff")
	}
	if strings.TrimSpace(rec.Body.String()) != `{"n":1}` {
		t.Errorf("тело %q", rec.Body.String())
	}
}

func TestErrors(t *testing.T) {
	cases := []struct {
		name   string
		err    error
		status int
		code   string
		text   string
	}{
		{"ошибка ядра", core.ErrNotFound, 404, "E1001", "ru:err.not_found"},
		{"обёрнутая ошибка ядра", errors.Join(errors.New("ctx"), core.ErrForbidden), 403, "E1002", "ru:err.forbidden"},
		{"забанен", core.ErrBanned, 403, "E3006", "ru:err.banned"},
		{"техработы", core.ErrMaintenance, 503, "E1005", "ru:err.maintenance"},
		{"неверные данные", core.ErrInvalid, 400, "E1003", "ru:err.invalid"},
		{"не хватает денег", &core.NeedMoney{Missing: 15000}, 402, "E2001", "ru:err.not_enough_money"},
		{"сессия закрыта", core.ErrSessionGone, 401, "E6008", "ru:err.session_gone"},
		{"много попыток", core.ErrTooManyTries, 429, "E5007", "ru:err.too_many_tries"},
		{"прочая ошибка ядра", core.ErrTrialUsed, 409, "E3005", "ru:err.trial_used"},
		{"сбой базы", errors.New("sql: disk I/O error at /var/lib/worker/worker.db"), 500, "E1006", "ru:err.internal"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := newTestServer(t)
			s.Handle("GET /api/x", func(w http.ResponseWriter, r *http.Request) (any, error) { return nil, c.err })
			rec := do(s, "GET", "/api/x", "")
			if rec.Code != c.status {
				t.Fatalf("статус %d, ждали %d", rec.Code, c.status)
			}
			var got struct{ Code, Text string }
			if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			if got.Code != c.code || got.Text != c.text {
				t.Errorf("тело %+v", got)
			}
			body := rec.Body.String()
			for _, leak := range []string{"sql", "/var/lib", "disk"} {
				if strings.Contains(body, leak) {
					t.Errorf("в ответе подробности ошибки: %q", leak)
				}
			}
		})
	}
}

func TestErrorLang(t *testing.T) {
	s := newTestServer(t)
	s.Handle("GET /api/x", func(w http.ResponseWriter, r *http.Request) (any, error) { return nil, core.ErrNotFound })
	req := httptest.NewRequest("GET", "/api/x", nil)
	req = req.WithContext(WithLang(req.Context(), "en"))
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if !strings.Contains(rec.Body.String(), "en:err.not_found") {
		t.Errorf("текст не на языке абонента: %s", rec.Body)
	}
}

func TestInternalErrorLogged(t *testing.T) {
	var sb strings.Builder
	s := New(Deps{Text: fakeTexts, Log: slog.New(slog.NewTextHandler(&sb, nil))})
	s.Handle("GET /api/x", func(w http.ResponseWriter, r *http.Request) (any, error) { return nil, errors.New("sql: boom") })
	do(s, "GET", "/api/x", "")
	if !strings.Contains(sb.String(), "sql: boom") {
		t.Errorf("подробности не попали в журнал: %q", sb.String())
	}
}

func TestPanicIsInternalError(t *testing.T) {
	s := newTestServer(t)
	s.Handle("GET /api/x", func(w http.ResponseWriter, r *http.Request) (any, error) { panic("secret-value") })
	rec := do(s, "GET", "/api/x", "")
	if rec.Code != 500 || strings.Contains(rec.Body.String(), "secret-value") || !strings.Contains(rec.Body.String(), "E1006") {
		t.Errorf("паника: %d %s", rec.Code, rec.Body)
	}
}

func TestUnknownRoute(t *testing.T) {
	s := newTestServer(t)
	s.Handle("GET /api/x", func(w http.ResponseWriter, r *http.Request) (any, error) { return nil, nil })
	rec := do(s, "GET", "/api/nope", "")
	if rec.Code != 404 || !strings.Contains(rec.Body.String(), "E1001") {
		t.Errorf("неизвестный адрес: %d %s", rec.Code, rec.Body)
	}
	rec = do(s, "POST", "/api/x", "")
	if rec.Code != 404 && rec.Code != 405 {
		t.Errorf("чужой метод: %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("ошибка адреса не JSON: %q", ct)
	}
}

func TestNilResultIsEmptyObject(t *testing.T) {
	s := newTestServer(t)
	s.Handle("POST /api/x", func(w http.ResponseWriter, r *http.Request) (any, error) { return nil, nil })
	rec := do(s, "POST", "/api/x", "")
	if rec.Code != 200 || strings.TrimSpace(rec.Body.String()) != "{}" {
		t.Errorf("%d %q", rec.Code, rec.Body)
	}
}

func TestTypesListHasErrorBody(t *testing.T) {
	for _, typ := range Types() {
		if typ.Name() == "ErrorBody" {
			return
		}
	}
	t.Error("ErrorBody не отдан генератору типов")
}
