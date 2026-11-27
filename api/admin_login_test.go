package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/bini69-oi/worker/core"
)

func startLoginAPI(t *testing.T, e *adminEnv) (AdminLoginStarted, int64) {
	t.Helper()
	rec := e.post("", "/api/admin/login", fmt.Sprintf(`{"tg_id":%d,"password":%q}`, adminOwner, ownerPass))
	if rec.Code != 200 {
		t.Fatalf("вход: %d %s", rec.Code, rec.Body)
	}
	var st AdminLoginStarted
	if err := json.Unmarshal(rec.Body.Bytes(), &st); err != nil || st.ID == "" || st.Code == "" {
		t.Fatalf("ответ входа: %s", rec.Body)
	}
	var cid int64
	if _, err := fmt.Sscanf(st.ID, "%d.", &cid); err != nil {
		t.Fatal(err)
	}
	return st, cid
}

func TestAdminLoginSuccess(t *testing.T) {
	e := newAdminEnv(t)
	st, cid := startLoginAPI(t, e)

	rec := e.get("", "/api/admin/login/"+st.ID)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"pending"`) || rec.Header().Get("Set-Cookie") != "" {
		t.Fatalf("до нажатия: %d %s %q", rec.Code, rec.Body, rec.Header().Get("Set-Cookie"))
	}
	if err := e.c.AnswerLogin(context.Background(), cid, adminOwner, st.Code); err != nil {
		t.Fatal(err)
	}
	rec = e.get("", "/api/admin/login/"+st.ID)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"ok"`) {
		t.Fatalf("после нажатия: %d %s", rec.Code, rec.Body)
	}
	cookies := (&http.Response{Header: rec.Header()}).Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookie: %v", rec.Header())
	}
	c := cookies[0]
	if c.Name != AdminCookie || !c.HttpOnly || !c.Secure || c.SameSite != http.SameSiteStrictMode || c.Path != "/api/admin" {
		t.Errorf("атрибуты cookie: %+v", c)
	}
	if c.Name == SessionCookie {
		t.Errorf("cookie админки совпала с cookie кабинета")
	}
}

func TestAdminLoginWrongPassword(t *testing.T) {
	e := newAdminEnv(t)
	before := len(e.st.Tasks())
	rec := e.post("", "/api/admin/login", fmt.Sprintf(`{"tg_id":%d,"password":"неверный пароль"}`, adminOwner))
	if rec.Code != 401 || errCode(t, rec) != core.ErrBadLogin.Code {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if len(e.st.Tasks()) != before {
		t.Errorf("при неверном пароле в Telegram что-то ушло")
	}
	// Ошибка засчитана в защиту от перебора: пятая подряд блокирует вход даже с верным паролем.
	for range 4 {
		e.post("", "/api/admin/login", fmt.Sprintf(`{"tg_id":%d,"password":"неверный пароль"}`, adminOwner))
	}
	rec = e.post("", "/api/admin/login", fmt.Sprintf(`{"tg_id":%d,"password":%q}`, adminOwner, ownerPass))
	if rec.Code != 429 {
		t.Errorf("перебор не остановлен: %d %s", rec.Code, rec.Body)
	}
}

func TestAdminLoginDenied(t *testing.T) {
	e := newAdminEnv(t)
	st, cid := startLoginAPI(t, e)
	if err := e.c.AnswerLogin(context.Background(), cid, adminOwner, "00"); err != nil {
		t.Fatal(err)
	}
	rec := e.get("", "/api/admin/login/"+st.ID)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"denied"`) || rec.Header().Get("Set-Cookie") != "" {
		t.Errorf("%d %s", rec.Code, rec.Body)
	}
}

func TestAdminLoginBadID(t *testing.T) {
	e := newAdminEnv(t)
	for _, id := range []string{"abc", "1.", ".x", "1.чужой", "99999.xx"} {
		rec := e.get("", "/api/admin/login/"+id)
		if rec.Code != 401 && rec.Code != 400 {
			t.Errorf("id %q: %d %s", id, rec.Code, rec.Body)
		}
	}
}

func TestCabinetCookieDoesNotOpenAdmin(t *testing.T) {
	e := newAdminEnv(t)
	token := strings.TrimPrefix(e.owner, AdminCookie+"=")
	rec := e.get(SessionCookie+"="+token, "/api/admin/me")
	if rec.Code != 401 {
		t.Fatalf("cookie кабинета пустила в админку: %d %s", rec.Code, rec.Body)
	}
	if rec = e.get("", "/api/admin/me"); rec.Code != 401 {
		t.Fatalf("без cookie: %d", rec.Code)
	}
}

func TestAdminLoginNeedsOrigin(t *testing.T) {
	e := newAdminEnv(t)
	rec := e.post("", "/api/admin/login", `{`)
	if rec.Code != 400 {
		t.Errorf("битое тело: %d", rec.Code)
	}
}
