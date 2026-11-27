package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// startLogin начинает вход в браузере и отдаёт ответ сайта и nonce из ссылки бота.
func startLogin(t *testing.T, e *appEnv) (LoginStartResponse, string) {
	t.Helper()
	rec := e.req("POST", "/api/app/login", "{}")
	if rec.Code != 200 {
		t.Fatalf("начало входа: %d %s", rec.Code, rec.Body)
	}
	var st LoginStartResponse
	decode(t, rec, &st)
	nonce, ok := strings.CutPrefix(st.Link, "https://t.me/worker_bot?start=web_")
	if !ok || st.ID == "" || len(st.Code) != 2 {
		t.Fatalf("ответ входа: %+v", st)
	}
	// Id, код и ссылка случайные: в эталоне они заменены, форма ответа остаётся.
	fixed := LoginStartResponse{ID: "LOGIN-ID", Code: "42", Link: "https://t.me/worker_bot?start=web_NONCE", ExpiresAt: st.ExpiresAt}
	raw, _ := json.Marshal(fixed)
	golden(t, "login_start", raw)
	return st, nonce
}

func poll(e *appEnv, id string) *httptest.ResponseRecorder {
	return e.req("GET", "/api/app/login/"+id, "")
}

func TestLoginBrowserFlow(t *testing.T) {
	e := newAppEnv(t)
	u, _ := e.c.Register(t.Context(), 55, "ivan", "ru")
	st, nonce := startLogin(t, e)
	var p LoginPollResponse
	rec := poll(e, st.ID)
	decode(t, rec, &p)
	if rec.Code != 200 || p.State != "pending" || cookieNamed(rec, SessionCookie) != nil {
		t.Fatalf("до нажатия: %d %+v", rec.Code, p)
	}
	golden(t, "login_pending", rec.Body.Bytes())
	prompt, err := e.c.WebLoginOpen(t.Context(), nonce, u.TgID)
	if err != nil {
		t.Fatal(err)
	}
	if a, err := e.c.WebLoginAnswer(t.Context(), prompt.ID, u.TgID, st.Code); err != nil || !a.Confirmed {
		t.Fatalf("нажатие: %v %v", a, err)
	}
	rec = poll(e, st.ID)
	decode(t, rec, &p)
	if p.State != "confirmed" {
		t.Fatalf("после нажатия: %+v", p)
	}
	golden(t, "login_confirmed", rec.Body.Bytes())
	c := cookieNamed(rec, SessionCookie)
	if c == nil {
		t.Fatal("cookie не поставлена")
	}
	if !c.HttpOnly || !c.Secure || c.SameSite != http.SameSiteStrictMode || c.Path != "/api/app" || c.MaxAge != 30*24*3600 {
		t.Fatalf("флаги cookie: %+v", c)
	}
	if strings.Contains(rec.Body.String(), c.Value) {
		t.Fatal("токен попал в тело ответа")
	}
	// Cookie даёт вход: выход с ней требует X-Worker, как любой изменяющий запрос.
	if out := e.req("POST", "/api/app/logout", "{}", "Cookie", SessionCookie+"="+c.Value); out.Code != 403 {
		t.Fatalf("выход без X-Worker: %d", out.Code)
	}
	out := e.req("POST", "/api/app/logout", "{}", "Cookie", SessionCookie+"="+c.Value, "X-Worker", "1")
	if out.Code != 200 {
		t.Fatalf("выход: %d %s", out.Code, out.Body)
	}
	if gone := cookieNamed(out, SessionCookie); gone == nil || gone.MaxAge >= 0 {
		t.Fatalf("cookie не стёрта: %+v", gone)
	}
	if again := e.req("POST", "/api/app/logout", "{}", "Cookie", SessionCookie+"="+c.Value, "X-Worker", "1"); again.Code != 401 {
		t.Fatalf("закрытая сессия: %d", again.Code)
	}
	// После окна повторной выдачи (30 секунд) сессия больше не выдаётся.
	e.clock.Add(31 * time.Second)
	rec = poll(e, st.ID)
	decode(t, rec, &p)
	if p.State != "expired" || cookieNamed(rec, SessionCookie) != nil {
		t.Fatalf("повторный опрос: %+v", p)
	}
}

func TestLoginRejectedAndExpired(t *testing.T) {
	e := newAppEnv(t)
	u, _ := e.c.Register(t.Context(), 55, "ivan", "ru")
	st, nonce := startLogin(t, e)
	prompt, _ := e.c.WebLoginOpen(t.Context(), nonce, u.TgID)
	if _, err := e.c.WebLoginAnswer(t.Context(), prompt.ID, u.TgID, "no"); err != nil {
		t.Fatal(err)
	}
	var p LoginPollResponse
	rec := poll(e, st.ID)
	decode(t, rec, &p)
	if p.State != "rejected" || cookieNamed(rec, SessionCookie) != nil {
		t.Fatalf("«Это не я»: %+v", p)
	}
	golden(t, "login_rejected", rec.Body.Bytes())
	st2, _ := startLogin(t, e)
	e.clock.Add(6 * time.Minute)
	rec = poll(e, st2.ID)
	decode(t, rec, &p)
	if p.State != "expired" {
		t.Fatalf("истёкший запрос: %+v", p)
	}
	golden(t, "login_expired", rec.Body.Bytes())
	if rec := poll(e, "чужой"); rec.Code != 200 {
		t.Fatalf("неизвестный id: %d", rec.Code)
	}
}

func TestLoginStartBody(t *testing.T) {
	e := newAppEnv(t)
	if rec := e.req("POST", "/api/app/login", `{"tg_id":5}`); rec.Code != 400 {
		t.Fatalf("лишнее поле: %d", rec.Code)
	}
}

func TestLoginStartLimitPerIP(t *testing.T) {
	e := newAppEnv(t)
	ok, limited := 0, 0
	for range 15 { // секунда между запросами: общий предел в секунду не мешает, проверяется именно предел входа
		switch rec := e.req("POST", "/api/app/login", "{}"); rec.Code {
		case 200:
			ok++
		case 429:
			limited++
		default:
			t.Fatalf("статус %d", rec.Code)
		}
		e.clock.Add(time.Second)
	}
	if ok != 10 || limited != 5 {
		t.Fatalf("принято %d, отклонено %d, ждали 10 и 5", ok, limited)
	}
	// С другого адреса вход открыт, через 10 минут с первого тоже.
	r := httptest.NewRequest("POST", "/api/app/login", strings.NewReader("{}"))
	r.RemoteAddr = "10.0.0.2:1"
	rec := httptest.NewRecorder()
	e.s.ServeHTTP(rec, r)
	if rec.Code != 200 {
		t.Fatalf("другой адрес: %d", rec.Code)
	}
	e.clock.Add(10 * time.Minute)
	if rec := e.req("POST", "/api/app/login", "{}"); rec.Code != 200 {
		t.Fatalf("после окна: %d", rec.Code)
	}
}

// Ответ на опрос потерялся по дороге: повторный опрос ставит новую cookie, а потерянная сессия не работает.
func TestLoginPollLostResponseReissues(t *testing.T) {
	e := newAppEnv(t)
	u, _ := e.c.Register(t.Context(), 55, "ivan", "ru")
	st, nonce := startLogin(t, e)
	prompt, _ := e.c.WebLoginOpen(t.Context(), nonce, u.TgID)
	if _, err := e.c.WebLoginAnswer(t.Context(), prompt.ID, u.TgID, st.Code); err != nil {
		t.Fatal(err)
	}
	lost := cookieNamed(poll(e, st.ID), SessionCookie)
	e.clock.Add(5 * time.Second)
	again := cookieNamed(poll(e, st.ID), SessionCookie)
	if lost == nil || again == nil || lost.Value == again.Value {
		t.Fatalf("повторной cookie нет: %v %v", lost, again)
	}
	if rec := e.req("GET", "/api/app/me", "", "Cookie", SessionCookie+"="+lost.Value); rec.Code != 401 {
		t.Fatalf("потерянная сессия жива: %d", rec.Code)
	}
	e.clock.Add(time.Second)
	if rec := e.req("GET", "/api/app/me", "", "Cookie", SessionCookie+"="+again.Value); rec.Code != 200 {
		t.Fatalf("новая сессия: %d", rec.Code)
	}
}
