// Package web вшивает собранный фронт в бинарник и отдаёт его: кабинет на /app/, админку на /admin/,
// общие файлы на /assets/. Без сборки (make web) бинарник тоже собирается и отвечает страницей «Фронт не собран».
package web

import (
	"bytes"
	"embed"
	"io/fs"
	"net/http"
	"path"
	"regexp"
	"strings"
	"time"
)

// В git лежит только build/README.txt, чтобы go build проходил без Node.js. `all:` берёт и файлы с точкой в имени.
//
//go:embed all:build
var embedded embed.FS

const notBuilt = `<!doctype html>
<html lang="ru"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">
<title>Worker</title></head>
<body><p>Фронт не собран: выполните make web</p></body></html>
`

// hashed это файл сборки с хэшем в имени: assets/app-DkQ3xYzA.js. Такой файл с новой версией получает новое имя,
// поэтому его можно кэшировать на год.
var hashed = regexp.MustCompile(`^assets/[^/]+-[A-Za-z0-9_-]{8,}\.[A-Za-z0-9]+$`)

// Handler отдаёт вшитый фронт. Подключается на "/app/", "/admin/" и "/assets/".
func Handler() http.Handler {
	sub, err := fs.Sub(embedded, "build")
	if err != nil {
		panic(err) // имя папки в go:embed и здесь одно: ошибка может быть только при правке кода
	}
	return newHandler(sub)
}

func newHandler(fsys fs.FS) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		secure(w.Header(), r.URL.Path)
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		name, ok := filePath(r.URL.Path)
		if !ok {
			if p := strings.Trim(r.URL.Path, "/"); p == "app" || p == "admin" {
				http.Redirect(w, r, "/"+p+"/", http.StatusMovedPermanently)
				return
			}
			http.NotFound(w, r)
			return
		}
		section, _, _ := strings.Cut(name, "/")
		if section != "assets" {
			if _, err := fs.Stat(fsys, section+"/index.html"); err != nil {
				serve(w, r, "index.html", []byte(notBuilt), http.StatusServiceUnavailable, "no-store")
				return
			}
		}
		data, err := fs.ReadFile(fsys, name)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		cache := "no-cache"
		if hashed.MatchString(name) {
			cache = "public, max-age=31536000, immutable"
		}
		serve(w, r, name, data, http.StatusOK, cache)
	})
}

// Политики безопасности страниц. Кабинету нужен скрипт Telegram и показ во фрейме Telegram Web,
// админке ни то ни другое: её нельзя встроить никуда.
const (
	appCSP   = "default-src 'self'; script-src 'self' https://telegram.org; frame-ancestors https://web.telegram.org"
	adminCSP = "default-src 'self'; script-src 'self'; frame-ancestors 'none'"
)

// secure ставит заголовки безопасности на любой ответ, в том числе 404 и заглушку.
func secure(h http.Header, urlPath string) {
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Referrer-Policy", "no-referrer")
	switch {
	case strings.HasPrefix(urlPath, "/app"):
		h.Set("Content-Security-Policy", appCSP)
	case strings.HasPrefix(urlPath, "/admin"):
		h.Set("Content-Security-Policy", adminCSP)
	}
}

// filePath превращает адрес в путь внутри сборки. Отдаём только app/, admin/ и assets/: остальное в build
// (README.txt, манифест) наружу не уходит. Адрес папки это её index.html.
func filePath(urlPath string) (string, bool) {
	if !strings.HasPrefix(urlPath, "/") || strings.Contains(urlPath, "..") {
		return "", false
	}
	name := strings.TrimPrefix(path.Clean(urlPath), "/")
	if strings.HasSuffix(urlPath, "/") {
		name = path.Join(name, "index.html")
	}
	section, rest, found := strings.Cut(name, "/")
	if !found || rest == "" || strings.HasPrefix(rest, ".") {
		return "", false
	}
	switch section {
	case "app", "admin":
		return name, true
	case "assets":
		return name, !strings.HasSuffix(urlPath, "/")
	}
	return "", false
}

func serve(w http.ResponseWriter, r *http.Request, name string, data []byte, status int, cache string) {
	h := w.Header()
	h.Set("Cache-Control", cache)
	if status != http.StatusOK {
		h.Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(status)
		if r.Method != http.MethodHead {
			_, _ = w.Write(data) // ошибка записи значит, что клиент ушёл
		}
		return
	}
	http.ServeContent(w, r, name, time.Time{}, bytes.NewReader(data))
}
