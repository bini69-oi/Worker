package core_test

import (
	"strings"
	"testing"

	"github.com/bini69-oi/worker/core"
)

func TestClientDevice(t *testing.T) {
	cases := []struct{ name, ua, want string }{
		{"Chrome Windows", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36", "Chrome, Windows"},
		{"Edge", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/126.0.0.0 Safari/537.36 Edg/126.0.0.0", "Edge, Windows"},
		{"Firefox Linux", "Mozilla/5.0 (X11; Ubuntu; Linux x86_64; rv:127.0) Gecko/20100101 Firefox/127.0", "Firefox, Linux"},
		{"Safari Mac", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.5 Safari/605.1.15", "Safari, macOS"},
		{"Safari iPhone", "Mozilla/5.0 (iPhone; CPU iPhone OS 17_5 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.5 Mobile/15E148 Safari/604.1", "Safari, iOS"},
		{"Chrome Android", "Mozilla/5.0 (Linux; Android 14; Pixel 8) AppleWebKit/537.36 Chrome/126.0.0.0 Mobile Safari/537.36", "Chrome, Android"},
		{"Яндекс", "Mozilla/5.0 (Windows NT 10.0) AppleWebKit/537.36 Chrome/124.0 YaBrowser/24.6 Safari/537.36", "Yandex Browser, Windows"},
		{"Opera", "Mozilla/5.0 (Windows NT 10.0) AppleWebKit/537.36 Chrome/124.0 Safari/537.36 OPR/110.0", "Opera, Windows"},
		{"пусто", "", ""},
		{"своя строка", "Ваш аккаунт взломан, нажмите 42 <a href=evil>тут</a>", ""},
		{"только система", "curl/8.0 (Windows NT 10.0)", "Windows"},
	}
	for _, c := range cases {
		if got := core.ClientDevice(c.ua).String(); got != c.want {
			t.Errorf("%s: %q, ждали %q", c.name, got, c.want)
		}
	}
}

// Что бы клиент ни прислал, из строки берутся только слова белого списка.
func FuzzClientDevice(f *testing.F) {
	f.Add("Mozilla/5.0 (Windows NT 10.0) Chrome/1")
	f.Add("<b>evil</b> Firefox/1 Android")
	f.Fuzz(func(t *testing.T, ua string) {
		d := core.ClientDevice(ua)
		ok := map[string]bool{"": true, "Edge": true, "Opera": true, "Yandex Browser": true, "Samsung Internet": true, "Firefox": true, "Chrome": true, "Safari": true,
			"iOS": true, "Android": true, "Windows": true, "ChromeOS": true, "macOS": true, "Linux": true}
		if !ok[d.Browser] || !ok[d.OS] || strings.ContainsAny(d.String(), "<>&\"") {
			t.Fatalf("%q дало %+v", ua, d)
		}
	})
}
