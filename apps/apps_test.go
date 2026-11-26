package apps

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/bini69-oi/worker/core"
)

var (
	key = []byte(strings.Repeat("s", 32))
	t0  = time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
)

func TestListAndCheck(t *testing.T) {
	if got := List("happ,v2raytun"); len(got) != 2 || got[0].ID != "happ" {
		t.Fatalf("умолчания: %+v", got)
	}
	if got := List(" hiddify , nope"); len(got) != 1 || got[0].ID != "hiddify" {
		t.Fatalf("неизвестный id не пропущен: %+v", got)
	}
	if err := Check("happ,nope"); err == nil || !strings.Contains(err.Error(), "happ") {
		t.Fatalf("неизвестный id принят или нет подсказки: %v", err)
	}
	if err := Check(" , "); err == nil {
		t.Fatal("пустой список принят")
	}
	if err := Check("happ"); err != nil {
		t.Fatal(err)
	}
}

func TestToken(t *testing.T) {
	tok := Token(key, 42, t0)
	if id, err := Parse(key, tok, t0.Add(23*time.Hour)); err != nil || id != 42 {
		t.Fatalf("живой токен: %d %v", id, err)
	}
	if strings.Contains(tok, "42") {
		t.Fatalf("id виден в токене: %s", tok)
	}
	if _, err := Parse(key, tok, t0.Add(25*time.Hour)); err != ErrExpired {
		t.Fatalf("старый токен: %v", err)
	}
	if _, err := Parse([]byte(strings.Repeat("x", 32)), tok, t0); err != ErrToken {
		t.Fatalf("чужой ключ: %v", err)
	}
	b := []byte(tok)
	b[3] ^= 1
	if _, err := Parse(key, string(b), t0); err != ErrToken {
		t.Fatalf("испорченный токен: %v", err)
	}
	if Token(key, 1, t0) == Token(key, 2, t0) {
		t.Fatal("токены разных абонентов совпали")
	}
}

func FuzzParse(f *testing.F) {
	f.Add(Token(key, 42, t0))
	f.Add("")
	f.Add("AAAA")
	f.Fuzz(func(t *testing.T, s string) {
		if id, err := Parse(key, s, t0); err == nil && id <= 0 {
			t.Fatalf("принят токен с id %d", id)
		}
	})
}

type fakeSubs map[int64]core.Subscription

func (f fakeSubs) Sub(_ context.Context, id int64) (core.Subscription, error) {
	s, ok := f[id]
	if !ok {
		return s, core.ErrNotFound
	}
	return s, nil
}

func get(h http.Handler, path string) *httptest.ResponseRecorder {
	mux := http.NewServeMux()
	mux.Handle("GET /dl/{app}/{token}", h)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
	return w
}

func TestHandler(t *testing.T) {
	now := t0
	subs := fakeSubs{42: {SubURL: "https://sub.test/abc"}, 43: {}}
	h := Handler(subs, key, func() time.Time { return now }, func(k string) string { return k })

	w := get(h, "/dl/happ/"+Token(key, 42, t0))
	if w.Code != http.StatusFound || w.Header().Get("Location") != "happ://add/https://sub.test/abc" {
		t.Fatalf("переход: %d %q", w.Code, w.Header().Get("Location"))
	}
	// После перевыпуска старая кнопка ведёт на новую ссылку.
	subs[42] = core.Subscription{SubURL: "https://sub.test/new"}
	if w = get(h, "/dl/happ/"+Token(key, 42, t0)); w.Header().Get("Location") != "happ://add/https://sub.test/new" {
		t.Fatalf("после перевыпуска: %q", w.Header().Get("Location"))
	}
	if w = get(h, "/dl/happ/"+Token(key, 43, t0)); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "dl.pending") {
		t.Fatalf("ссылки ещё нет: %d %s", w.Code, w.Body.String())
	}
	if w = get(h, "/dl/nope/"+Token(key, 42, t0)); w.Code != http.StatusNotFound {
		t.Fatalf("неизвестное приложение: %d", w.Code)
	}
	if w = get(h, "/dl/happ/garbage"); w.Code != http.StatusNotFound {
		t.Fatalf("подделка: %d", w.Code)
	}
	now = t0.Add(25 * time.Hour)
	if w = get(h, "/dl/happ/"+Token(key, 42, t0)); w.Code != http.StatusGone || !strings.Contains(w.Body.String(), "dl.expired") {
		t.Fatalf("устарел: %d %s", w.Code, w.Body.String())
	}
}
