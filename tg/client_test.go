package tg

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

const testToken = "123456:SECRET-token"

func newTestClient(srv *httptest.Server) *Client {
	return &Client{Token: testToken, BaseURL: srv.URL, HTTP: srv.Client()}
}

func TestCallOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/bot"+testToken+"/getMe" || r.Method != http.MethodPost {
			t.Errorf("путь %s метод %s", r.URL.Path, r.Method)
		}
		_, _ = w.Write([]byte(`{"ok":true,"result":{"id":99,"is_bot":true,"first_name":"Bot","username":"b_bot"}}`))
	}))
	defer srv.Close()
	u, err := newTestClient(srv).GetMe(context.Background())
	if err != nil || u.ID != 99 || u.Username != "b_bot" {
		t.Fatalf("u=%+v err=%v", u, err)
	}
}

func TestCallAPIError(t *testing.T) {
	cases := []struct {
		name, body string
		code       int
		blocked    bool
		notFound   bool
		notMod     bool
	}{
		{"заблокировал", `{"ok":false,"error_code":403,"description":"Forbidden: bot was blocked by the user"}`, 403, true, false, false},
		{"аккаунт удалён", `{"ok":false,"error_code":403,"description":"Forbidden: user is deactivated"}`, 403, true, false, false},
		{"403 не про блок", `{"ok":false,"error_code":403,"description":"Forbidden: bot is not a member"}`, 403, false, false, false},
		{"нет сообщения", `{"ok":false,"error_code":400,"description":"Bad Request: message to edit not found"}`, 400, false, true, false},
		{"нет темы", `{"ok":false,"error_code":400,"description":"Bad Request: message thread not found"}`, 400, false, true, false},
		{"нельзя править", `{"ok":false,"error_code":400,"description":"Bad Request: message can't be edited"}`, 400, false, true, false},
		{"не изменено", `{"ok":false,"error_code":400,"description":"Bad Request: message is not modified: specified new message content"}`, 400, false, false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.code)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()
			_, err := newTestClient(srv).GetMe(context.Background())
			var te *Error
			if !errors.As(err, &te) || te.Code != tc.code || te.Description == "" {
				t.Fatalf("err=%v", err)
			}
			if te.IsBlocked() != tc.blocked || te.IsNotFound() != tc.notFound || te.IsNotModified() != tc.notMod {
				t.Fatalf("классы ошибки неверны: %+v", te)
			}
		})
	}
}

func TestCallNotJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("<html>bad gateway</html>"))
	}))
	defer srv.Close()
	_, err := newTestClient(srv).GetMe(context.Background())
	var te *Error
	if !errors.As(err, &te) || te.Code != 502 {
		t.Fatalf("err=%v", err)
	}
}

func TestCallNetworkErrorHasNoToken(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	c := newTestClient(srv)
	srv.Close()
	_, err := c.GetMe(context.Background())
	if err == nil {
		t.Fatal("ожидали ошибку сети")
	}
	if strings.Contains(err.Error(), "SECRET") || strings.Contains(err.Error(), testToken) || strings.Contains(err.Error(), "/bot") {
		t.Fatalf("в ошибке токен или URL: %v", err)
	}
}

func TestCallContextCanceled(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := newTestClient(srv).GetMe(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v", err)
	}
}

// fakeClock это часы, которые двигает Sleep.
type fakeClock struct {
	mu     sync.Mutex
	t      time.Time
	sleeps []time.Duration
}

func (f *fakeClock) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.t
}

func (f *fakeClock) Sleep(ctx context.Context, d time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sleeps = append(f.sleeps, d)
	f.t = f.t.Add(d)
	return nil
}

func TestRetryAfter429(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		if calls < 3 {
			w.WriteHeader(429)
			_, _ = w.Write([]byte(`{"ok":false,"error_code":429,"description":"Too Many Requests","parameters":{"retry_after":3}}`))
			return
		}
		_, _ = w.Write([]byte(`{"ok":true,"result":{"id":1}}`))
	}))
	defer srv.Close()
	fc := &fakeClock{t: time.Unix(1000, 0)}
	c := newTestClient(srv)
	c.Now, c.Sleep = fc.Now, fc.Sleep
	if _, err := c.GetMe(context.Background()); err != nil {
		t.Fatal(err)
	}
	if calls != 3 || len(fc.sleeps) != 2 || fc.sleeps[0] != 3*time.Second || fc.sleeps[1] != 3*time.Second {
		t.Fatalf("calls=%d sleeps=%v", calls, fc.sleeps)
	}
}

func TestRetryGivesUpAfterThree(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.WriteHeader(429)
		_, _ = w.Write([]byte(`{"ok":false,"error_code":429,"description":"Too Many Requests","parameters":{"retry_after":1}}`))
	}))
	defer srv.Close()
	fc := &fakeClock{t: time.Unix(1000, 0)}
	c := newTestClient(srv)
	c.Now, c.Sleep = fc.Now, fc.Sleep
	_, err := c.GetMe(context.Background())
	var te *Error
	if !errors.As(err, &te) || te.Code != 429 || te.RetryAfter != 1 || calls != 3 {
		t.Fatalf("err=%v calls=%d", err, calls)
	}
}

func TestRetrySleepCanceled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(429)
		_, _ = w.Write([]byte(`{"ok":false,"error_code":429,"description":"x","parameters":{"retry_after":5}}`))
	}))
	defer srv.Close()
	ctx, cancel := context.WithCancel(context.Background())
	c := newTestClient(srv)
	c.Sleep = func(ctx context.Context, _ time.Duration) error {
		cancel()
		return ctx.Err()
	}
	if _, err := c.GetMe(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v", err)
	}
}

func TestRateLimit25PerSecond(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true,"result":{"id":1}}`))
	}))
	defer srv.Close()
	fc := &fakeClock{t: time.Unix(1000, 0)}
	c := newTestClient(srv)
	c.Now, c.Sleep = fc.Now, fc.Sleep
	start := fc.Now()
	for i := 0; i < 25; i++ {
		if _, err := c.GetMe(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if len(fc.sleeps) != 0 {
		t.Fatalf("первые 25 не должны ждать: %v", fc.sleeps)
	}
	if _, err := c.GetMe(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(fc.sleeps) != 1 || fc.Now().Sub(start) < time.Second {
		t.Fatalf("26-й должен дождаться секунды: sleeps=%v", fc.sleeps)
	}
}
