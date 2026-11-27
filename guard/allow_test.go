package guard

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAllowed(t *testing.T) {
	cases := []struct {
		name, list, ip string
		want           bool
	}{
		{"пусто пускает всех", "", "198.51.100.1:0", true},
		{"пробелы это пусто", "  ", "198.51.100.1", true},
		{"точный адрес", "203.0.113.5", "203.0.113.5:4000", true},
		{"чужой адрес", "203.0.113.5", "203.0.113.6:4000", false},
		{"сеть", "10.0.0.0/8", "10.20.30.40:1", true},
		{"вне сети", "10.0.0.0/8", "11.0.0.1:1", false},
		{"несколько записей", "203.0.113.5, 10.0.0.0/8", "10.1.1.1:0", true},
		{"IPv6", "2001:db8::/32", "[2001:db8::5]:1", true},
		{"IPv6 вне сети", "2001:db8::/32", "[2001:db9::5]:1", false},
		{"IPv4 в IPv6-записи", "203.0.113.5", "[::ffff:203.0.113.5]:1", true},
		{"битая запись никого не пускает", "ivan", "203.0.113.5:1", false},
		{"битый адрес клиента", "203.0.113.5", "не адрес", false},
	}
	for _, c := range cases {
		if got := Allowed(c.list, c.ip); got != c.want {
			t.Errorf("%s: %v, ждали %v", c.name, got, c.want)
		}
	}
}

func TestOnlyFrom(t *testing.T) {
	list := "203.0.113.5"
	h := OnlyFrom(func() string { return list }, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) }))
	get := func(ip string) int {
		req := httptest.NewRequest("GET", "/admin/", nil)
		req.RemoteAddr = ip
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code
	}
	if get("203.0.113.5:1") != 200 || get("198.51.100.1:1") != 404 {
		t.Fatal("список не применяется")
	}
	list = "" // правка настройки действует сразу
	if get("198.51.100.1:1") != 200 {
		t.Error("пустой список должен пускать всех")
	}
}
