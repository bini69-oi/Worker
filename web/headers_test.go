package web

import (
	"strings"
	"testing"
	"testing/fstest"
)

func TestAppHeaders(t *testing.T) {
	h := newHandler(built())
	rec := get(h, "GET", "/app/")
	want := map[string]string{
		"Content-Security-Policy": "default-src 'self'; script-src 'self' https://telegram.org; frame-ancestors https://web.telegram.org",
		"X-Content-Type-Options":  "nosniff",
		"Referrer-Policy":         "no-referrer",
	}
	for k, v := range want {
		if got := rec.Header().Get(k); got != v {
			t.Errorf("%s: %q, ждали %q", k, got, v)
		}
	}
	if rec.Header().Get("X-Frame-Options") != "" {
		t.Errorf("X-Frame-Options запретил бы фрейм Telegram Web")
	}
}

func TestAdminHeaders(t *testing.T) {
	rec := get(newHandler(built()), "GET", "/admin/")
	got := rec.Header().Get("Content-Security-Policy")
	if got != "default-src 'self'; script-src 'self'; frame-ancestors 'none'" {
		t.Errorf("CSP админки: %q", got)
	}
	// Админка не встраивается и не грузит чужие скрипты, в том числе скрипт Telegram.
	for _, bad := range []string{"telegram.org", "unsafe-inline", "unsafe-eval", "frame-ancestors https"} {
		if strings.Contains(got, bad) {
			t.Errorf("в CSP админки лишнее %q", bad)
		}
	}
	if rec.Header().Get("X-Frame-Options") != "" && rec.Header().Get("X-Frame-Options") != "DENY" {
		t.Errorf("X-Frame-Options: %q", rec.Header().Get("X-Frame-Options"))
	}
	if rec.Header().Get("Referrer-Policy") != "no-referrer" || rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Errorf("нет Referrer-Policy или nosniff")
	}
}

func TestAssetsAndStubHeaders(t *testing.T) {
	rec := get(newHandler(built()), "GET", "/assets/app-DkQ3xYzA.js")
	if rec.Header().Get("X-Content-Type-Options") != "nosniff" || rec.Header().Get("Referrer-Policy") != "no-referrer" {
		t.Errorf("файлы сборки без nosniff или Referrer-Policy")
	}
	stub := get(newHandler(fstest.MapFS{"README.txt": {}}), "GET", "/app/")
	if stub.Header().Get("Content-Security-Policy") == "" {
		t.Errorf("у заглушки тоже есть CSP")
	}
}

func TestNotFoundHasHeaders(t *testing.T) {
	rec := get(newHandler(built()), "GET", "/app/nope.js")
	if rec.Header().Get("Referrer-Policy") != "no-referrer" {
		t.Errorf("у 404 нет Referrer-Policy")
	}
}
