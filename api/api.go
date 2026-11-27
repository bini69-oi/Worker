// Package api это HTTP API для кабинета и админки: роутер, JSON-ответы, ошибки одним видом, разбор тела.
// API ничего не решает само: проверяет форму запроса и зовёт ядро.
package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/bini69-oi/worker/core"
	"github.com/bini69-oi/worker/guard"
)

// Handler это обработчик адреса. Вернул значение: оно уйдёт JSON с кодом 200 (nil даёт `{}`).
// Вернул ошибку: она уйдёт телом `{"code", "text"}` (см. Fail).
type Handler func(w http.ResponseWriter, r *http.Request) (any, error)

// Deps это то, что нужно серверу снаружи.
type Deps struct {
	// Text отдаёт текст по ключу на языке lang. Пустой lang значит язык по умолчанию.
	Text func(key, lang string) string
	// Log принимает подробности внутренних ошибок. Секреты чистит сам журнал.
	Log *slog.Logger
	// Now это часы для лимита запросов. Пусто: настоящее время.
	Now func() time.Time
	// SessionID отдаёт номер ПРОВЕРЕННОЙ сессии запроса, пусто если сессии нет или токен неверный. По нему считается
	// частота: неизвестный токен делит счётчик со своим IP и не создаёт новых. Пусто в Deps: всё считается по IP.
	SessionID func(r *http.Request) string
}

// Server это роутер API. Один на кабинет и админку: адреса заводятся через Handle.
type Server struct {
	deps  Deps
	mux   *http.ServeMux
	limit *guard.Limiter
}

// New собирает сервер без адресов.
func New(d Deps) *Server {
	if d.Log == nil {
		d.Log = slog.Default()
	}
	s := &Server{deps: d, mux: http.NewServeMux(),
		limit: &guard.Limiter{Max: sessionRate, Window: time.Second, Now: d.Now}}
	s.mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { s.fail(w, r, core.ErrNotFound) })
	return s
}

// Handle заводит адрес вида "GET /api/app/me".
func (s *Server) Handle(pattern string, h Handler) {
	s.mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
		r = r.WithContext(context.WithValue(r.Context(), memoKey{}, &reqMemo{}))
		defer func() {
			if v := recover(); v != nil {
				if v == http.ErrAbortHandler {
					panic(v)
				}
				s.deps.Log.Error("паника в обработчике API", "route", pattern, "panic", v)
				s.fail(w, r, core.ErrInternal)
			}
		}()
		if err := s.admit(r); err != nil {
			s.fail(w, r, err)
			return
		}
		res, err := h(w, r)
		if err != nil {
			s.fail(w, r, err)
			return
		}
		if res == nil {
			res = struct{}{}
		}
		s.write(w, http.StatusOK, res)
	})
}

// ServeHTTP делает Server обработчиком для http.ServeMux.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) { s.mux.ServeHTTP(w, r) }

// reqMemo хранит то, что обработчикам нужно посчитать один раз на запрос, например проверку сессии:
// счётчик частоты и сам обработчик спрашивают о ней по очереди, а в базу идёт один запрос.
type reqMemo struct {
	once sync.Once
	auth Auth
	err  error
}

type memoKey struct{}

func memoOf(ctx context.Context) *reqMemo {
	m, _ := ctx.Value(memoKey{}).(*reqMemo)
	return m
}

type langKey struct{}

// WithLang кладёт язык абонента в контекст запроса: на нём пишутся тексты ошибок.
func WithLang(ctx context.Context, lang string) context.Context {
	return context.WithValue(ctx, langKey{}, lang)
}

func langOf(ctx context.Context) string {
	l, _ := ctx.Value(langKey{}).(string)
	return l
}

// write пишет JSON. Ответ API не кэшируется и не угадывается браузером как другой тип.
func (s *Server) write(w http.ResponseWriter, status int, v any) {
	body, err := json.Marshal(v)
	if err != nil {
		s.deps.Log.Error("ответ API не собрался в JSON", "err", err)
		status, body = http.StatusInternalServerError, []byte(`{"code":"`+core.ErrInternal.Code+`","text":""}`)
	}
	h := w.Header()
	h.Set("Content-Type", "application/json")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	if _, err := w.Write(append(body, '\n')); err != nil {
		s.deps.Log.Debug("клиент не дочитал ответ", "err", err)
	}
}
