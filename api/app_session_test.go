package api

import (
	"encoding/json"
	"testing"
	"time"
)

func tgBody(data string) string {
	b, _ := json.Marshal(map[string]string{"init_data": data})
	return string(b)
}

func TestSessionTgFresh(t *testing.T) {
	e := newAppEnv(t)
	rec := e.req("POST", "/api/app/session/tg", tgBody(initData(77, "en", e.clock.Now().Add(-5*time.Minute))))
	if rec.Code != 200 {
		t.Fatalf("свежие данные: %d %s", rec.Code, rec.Body)
	}
	var out SessionResponse
	decode(t, rec, &out)
	if out.Token == "" || out.Lang != "en" {
		t.Fatalf("ответ: %+v", out)
	}
	if u, err := e.c.UserByTg(t.Context(), 77); err != nil || u.Lang != "en" {
		t.Fatalf("абонент не создан: %v", err)
	}
	if cookieNamed(rec, SessionCookie) != nil {
		t.Fatal("Mini App получил cookie: токен идёт заголовком")
	}
	// Повторный вход находит того же абонента.
	rec2 := e.req("POST", "/api/app/session/tg", tgBody(initData(77, "en", e.clock.Now())))
	if rec2.Code != 200 {
		t.Fatalf("повторный вход: %d", rec2.Code)
	}
}

func TestSessionTgForged(t *testing.T) {
	e := newAppEnv(t)
	good := initData(77, "ru", e.clock.Now())
	cases := map[string]string{
		"чужой id":     replaceUser(good, 78),
		"чужой токен":  signInitData(map[string]string{"auth_date": "1773144000", "user": `{"id":77}`}, "999:OTHER"),
		"без подписи":  "auth_date=1773144000&user=%7B%22id%22%3A77%7D",
		"пусто":        "",
		"не разобрать": "%zz",
	}
	for name, data := range cases {
		rec := e.req("POST", "/api/app/session/tg", tgBody(data))
		if rec.Code != 401 {
			t.Errorf("%s: статус %d, ждали 401", name, rec.Code)
		}
	}
	if _, err := e.c.UserByTg(t.Context(), 78); err == nil {
		t.Fatal("подделка зарегистрировала абонента")
	}
}

func TestSessionTgOld(t *testing.T) {
	e := newAppEnv(t)
	rec := e.req("POST", "/api/app/session/tg", tgBody(initData(77, "ru", e.clock.Now().Add(-61*time.Minute))))
	if rec.Code != 401 {
		t.Fatalf("старые данные: %d", rec.Code)
	}
	var body ErrorBody
	decode(t, rec, &body)
	if body.Code != "E6040" {
		t.Fatalf("код %s", body.Code)
	}
}

func TestSessionTgBannedAndUnknownField(t *testing.T) {
	e := newAppEnv(t)
	e.login(77)
	u, _ := e.c.UserByTg(t.Context(), 77)
	if err := e.c.Ban(t.Context(), testOwner, u.ID, ""); err != nil {
		t.Fatal(err)
	}
	if rec := e.req("POST", "/api/app/session/tg", tgBody(initData(77, "ru", e.clock.Now()))); rec.Code != 403 {
		t.Fatalf("забаненный: %d", rec.Code)
	}
	if rec := e.req("POST", "/api/app/session/tg", `{"init_data":"x","user_id":1}`); rec.Code != 400 {
		t.Fatalf("лишнее поле: %d", rec.Code)
	}
}

// replaceUser меняет user.id в подписанных данных, не трогая подпись.
func replaceUser(data string, id int64) string {
	return strReplace(data, "%22id%22%3A77", "%22id%22%3A78")
}
