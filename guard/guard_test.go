package guard

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func seen(remote string, headers map[string]string) string {
	var got string
	h := RealIP(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { got = Host(r.RemoteAddr) }))
	r := httptest.NewRequest(http.MethodPost, "/pay/x", nil)
	r.RemoteAddr = remote
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	h.ServeHTTP(httptest.NewRecorder(), r)
	return got
}

func TestRealIP(t *testing.T) {
	cases := []struct {
		remote  string
		headers map[string]string
		want    string
	}{
		{"127.0.0.1:5000", map[string]string{"X-Real-IP": "185.71.76.5"}, "185.71.76.5"},
		{"10.0.0.2:5000", map[string]string{"X-Forwarded-For": "1.1.1.1, 185.71.76.5"}, "185.71.76.5"},
		// Напрямую из интернета заголовок не действует.
		{"203.0.113.9:5000", map[string]string{"X-Real-IP": "185.71.76.5"}, "203.0.113.9"},
		// Мусор в заголовке не действует.
		{"127.0.0.1:5000", map[string]string{"X-Real-IP": "evil"}, "127.0.0.1"},
	}
	for _, c := range cases {
		if got := seen(c.remote, c.headers); got != c.want {
			t.Errorf("%s %v: ждали %s, получили %s", c.remote, c.headers, c.want, got)
		}
	}
}

func TestLimiter(t *testing.T) {
	now := time.Unix(0, 0)
	l := &Limiter{Max: 3, Window: time.Minute, Now: func() time.Time { return now }}
	for i := range 3 {
		if !l.Allow("a") {
			t.Fatalf("запрос %d отклонён", i+1)
		}
	}
	if l.Allow("a") {
		t.Fatal("четвёртый запрос за минуту пропущен")
	}
	if !l.Allow("b") {
		t.Fatal("другой IP отклонён")
	}
	now = now.Add(time.Minute)
	if !l.Allow("a") {
		t.Fatal("новое окно не началось")
	}
}

func TestLimiterMemoryBounded(t *testing.T) {
	now := time.Unix(0, 0)
	l := &Limiter{Max: 1, Window: time.Minute, Now: func() time.Time { return now }}
	for i := range maxTracked + 50 {
		l.Allow(time.Duration(i).String())
	}
	if len(l.hits) > maxTracked {
		t.Fatalf("счётчиков %d больше предела", len(l.hits))
	}
}
