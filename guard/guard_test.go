package guard

import (
	"fmt"
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

// Забитая живыми ключами память не закрывает вход новым: вытесняются самые старые, свежий ключ принимается.
func TestLimiterFullMemoryStillAcceptsNew(t *testing.T) {
	now := time.Unix(0, 0)
	l := &Limiter{Max: 1, Window: time.Hour, Now: func() time.Time { return now }}
	for i := range maxTracked {
		now = now.Add(time.Millisecond)
		l.Allow(fmt.Sprint("flood", i))
	}
	now = now.Add(time.Second)
	if !l.Allow("honest") {
		t.Fatal("новый ключ получил отказ из-за забитой памяти")
	}
	if l.Allow("honest") {
		t.Fatal("новый ключ не считается после вытеснения")
	}
	if len(l.hits) > maxTracked {
		t.Fatalf("счётчиков %d", len(l.hits))
	}
	if _, old := l.hits["flood0"]; old {
		t.Fatal("самый старый ключ не вытеснен")
	}
	if _, fresh := l.hits[fmt.Sprint("flood", maxTracked-1)]; !fresh {
		t.Fatal("вытеснен свежий ключ")
	}
}

// Просроченные окна уходят первыми: живые счётчики при этом остаются.
func TestLimiterEvictsExpiredFirst(t *testing.T) {
	now := time.Unix(0, 0)
	l := &Limiter{Max: 1, Window: time.Minute, Now: func() time.Time { return now }}
	for i := range maxTracked - 1 {
		l.Allow(fmt.Sprint("old", i))
	}
	now = now.Add(30 * time.Second)
	l.Allow("live")
	now = now.Add(45 * time.Second) // old* просрочены, live ещё нет
	l.Allow("new")
	if _, ok := l.hits["live"]; !ok {
		t.Fatal("вытеснен живой счётчик, хотя были просроченные")
	}
}

func TestOver(t *testing.T) {
	now := time.Unix(0, 0)
	l := &Limiter{Max: 2, Window: time.Minute, Now: func() time.Time { return now }}
	if l.Over("a") {
		t.Fatal("пустой счётчик исчерпан")
	}
	l.Allow("a")
	if l.Over("a") {
		t.Fatal("один из двух")
	}
	l.Allow("a")
	if !l.Over("a") {
		t.Fatal("предел исчерпан, а Over говорит нет")
	}
	now = now.Add(time.Minute)
	if l.Over("a") {
		t.Fatal("окно прошло")
	}
}

// IPv6 из одной /64 это один ключ: перебор адресов внутри сети счётчик не обходит.
func TestKeyFoldsIPv6(t *testing.T) {
	a, b := Key("[2001:db8:1:2::1]:80"), Key("[2001:db8:1:2:ffff:ffff:ffff:ffff]:9")
	if a != b {
		t.Fatalf("одна /64, разные ключи: %s и %s", a, b)
	}
	if a == Key("[2001:db8:1:3::1]:80") {
		t.Fatal("разные /64 слиты в один ключ")
	}
	if Key("203.0.113.9:5000") != "203.0.113.9" || Key("203.0.113.9") != "203.0.113.9" {
		t.Fatal("IPv4 изменился")
	}
	if Key("[::ffff:203.0.113.9]:1") != "203.0.113.9" {
		t.Fatalf("IPv4 в IPv6-записи: %s", Key("[::ffff:203.0.113.9]:1"))
	}
	if Key("garbage") != "garbage" {
		t.Fatal("мусор должен остаться как есть")
	}
}
