package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func built() fstest.MapFS {
	return fstest.MapFS{
		"README.txt":                    {Data: []byte("заглушка")},
		"app/index.html":                {Data: []byte("<html>кабинет</html>")},
		"admin/index.html":              {Data: []byte("<html>админка</html>")},
		"assets/app-DkQ3xYzA.js":        {Data: []byte("console.log(1)")},
		"assets/app-DkQ3xYzA.css":       {Data: []byte("body{}")},
		"assets/logo.svg":               {Data: []byte("<svg/>")},
		"assets/chunk-a1B2c3D4_-9.js":   {Data: []byte("export {}")},
		"app/.vite/manifest.json":       {Data: []byte("{}")},
		"assets/sub/dir-ABCDEFGH.js":    {Data: []byte("x")},
		"assets/noext-ABCDEFGH":         {Data: []byte("x")},
		"assets/short-ab.js":            {Data: []byte("x")},
		"assets/with.dot-ABCDEFGH1.map": {Data: []byte("x")},
	}
}

func get(h http.Handler, method, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
	return rec
}

func TestNotBuilt(t *testing.T) {
	h := newHandler(fstest.MapFS{"README.txt": {Data: []byte("заглушка")}})
	for _, p := range []string{"/app/", "/admin/"} {
		rec := get(h, "GET", p)
		if rec.Code != http.StatusServiceUnavailable {
			t.Errorf("%s: статус %d", p, rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "Фронт не собран: выполните make web") {
			t.Errorf("%s: нет подсказки: %s", p, rec.Body)
		}
		if rec.Header().Get("Cache-Control") != "no-store" {
			t.Errorf("%s: заглушку нельзя кэшировать", p)
		}
		if !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/html") {
			t.Errorf("%s: тип %q", p, rec.Header().Get("Content-Type"))
		}
	}
}

func TestEmbeddedHandlerWorks(t *testing.T) {
	// Настоящая вшитая сборка: либо заглушка, либо кабинет, в любом случае страница отвечает.
	rec := get(Handler(), "GET", "/app/")
	if rec.Code != 200 && rec.Code != http.StatusServiceUnavailable {
		t.Errorf("статус %d", rec.Code)
	}
}

func TestServeBuilt(t *testing.T) {
	h := newHandler(built())
	rec := get(h, "GET", "/app/")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "кабинет") {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("тип %q", ct)
	}
	if rec = get(h, "GET", "/app/index.html"); rec.Code != 200 && rec.Code != http.StatusMovedPermanently {
		t.Errorf("index.html: %d", rec.Code)
	}
	if rec = get(h, "GET", "/admin/"); !strings.Contains(rec.Body.String(), "админка") {
		t.Errorf("админка: %d %s", rec.Code, rec.Body)
	}
	if rec = get(h, "HEAD", "/app/"); rec.Code != 200 || rec.Body.Len() != 0 {
		t.Errorf("HEAD: %d %q", rec.Code, rec.Body)
	}
	rec = get(h, "GET", "/app")
	if rec.Code != http.StatusMovedPermanently || rec.Header().Get("Location") != "/app/" {
		t.Errorf("/app без слэша: %d %q", rec.Code, rec.Header().Get("Location"))
	}
}

func TestCache(t *testing.T) {
	h := newHandler(built())
	year := "public, max-age=31536000, immutable"
	cases := []struct {
		path string
		want string
	}{
		{"/app/", "no-cache"},
		{"/admin/", "no-cache"},
		{"/assets/app-DkQ3xYzA.js", year},
		{"/assets/app-DkQ3xYzA.css", year},
		{"/assets/chunk-a1B2c3D4_-9.js", year},
		{"/assets/logo.svg", "no-cache"},
		{"/assets/short-ab.js", "no-cache"},
	}
	for _, c := range cases {
		rec := get(h, "GET", c.path)
		if rec.Code != 200 {
			t.Errorf("%s: статус %d", c.path, rec.Code)
			continue
		}
		if got := rec.Header().Get("Cache-Control"); got != c.want {
			t.Errorf("%s: Cache-Control %q, ждали %q", c.path, got, c.want)
		}
	}
}

func TestNotFoundAndMethods(t *testing.T) {
	h := newHandler(built())
	for _, p := range []string{"/app/nope.js", "/README.txt", "/", "/assets/", "/assets/../README.txt", "/app/../README.txt", "/other/", "/assets/missing-ABCDEFGH.js"} {
		if rec := get(h, "GET", p); rec.Code != http.StatusNotFound {
			t.Errorf("%s: статус %d, ждали 404", p, rec.Code)
		}
	}
	if rec := get(h, "POST", "/app/"); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST: %d", rec.Code)
	}
}
