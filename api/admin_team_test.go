package api

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/bini69-oi/worker/core"
)

func TestAdminTeamAddHelper(t *testing.T) {
	e := newAdminEnv(t)
	rec := e.post(e.owner, "/api/admin/team/add", `{"tg_id":71,"password":"helper password 1","perms":["users","support"]}`)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"pending":true`) {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	// До «Да» помощника нет.
	if strings.Contains(e.get(e.owner, "/api/admin/team").Body.String(), `"tg_id":71`) {
		t.Fatal("помощник появился до «Да»")
	}
	e.confirmLast(true)
	golden(t, "admin_team", e.get(e.owner, "/api/admin/team").Body.Bytes())

	cookie := e.session(71, "helper password 1")
	me := e.get(cookie, "/api/admin/me").Body.String()
	if !strings.Contains(me, `"users","support"`) {
		t.Errorf("права помощника: %s", me)
	}
	if rec := e.get(cookie, "/api/admin/payments"); rec.Code != 403 {
		t.Errorf("помощник открыл платежи: %d", rec.Code)
	}
	if rec := e.get(cookie, "/api/admin/team"); rec.Code != 403 {
		t.Errorf("помощник открыл команду: %d", rec.Code)
	}
}

func TestAdminTeamNoPerms(t *testing.T) {
	e := newAdminEnv(t)
	rec := e.post(e.owner, "/api/admin/team/add", `{"tg_id":72,"password":"helper password 1","perms":[]}`)
	if rec.Code != 400 || errCode(t, rec) != core.ErrInvalid.Code {
		t.Fatalf("без прав: %d %s", rec.Code, rec.Body)
	}
	rec = e.post(e.owner, "/api/admin/team/add", `{"tg_id":72,"password":"helper password 1","perms":["owner"]}`)
	if rec.Code != 400 {
		t.Errorf("право owner выдать нельзя: %d", rec.Code)
	}
	if rec = e.post(e.owner, "/api/admin/team/add", `{"tg_id":72,"password":"коротко","perms":["users"]}`); rec.Code != 400 {
		t.Errorf("короткий пароль: %d", rec.Code)
	}
}

func TestAdminTeamRemove(t *testing.T) {
	e := newAdminEnv(t)
	cookie := e.helper(73, core.PermUsers)
	if rec := e.post(e.owner, "/api/admin/team/remove", `{"tg_id":73}`); rec.Code != 200 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if rec := e.get(cookie, "/api/admin/me"); rec.Code != 200 {
		t.Fatalf("помощник потерял доступ до «Да»: %d", rec.Code)
	}
	e.confirmLast(true)
	if rec := e.get(cookie, "/api/admin/me"); rec.Code != 401 {
		t.Errorf("после «Да»: %d", rec.Code)
	}
}

func TestAdminSessionsCloseForeign(t *testing.T) {
	e := newAdminEnv(t)
	cookie := e.helper(74, core.PermUsers)
	var list AdminSessionList
	if err := json.Unmarshal(e.get(e.owner, "/api/admin/sessions").Body.Bytes(), &list); err != nil || len(list.Items) != 2 {
		t.Fatalf("сессии: %+v %v", list, err)
	}
	var foreign string
	for _, s := range list.Items {
		if s.AdminTg == 74 {
			foreign = s.ID
		}
		if s.AdminTg == adminOwner && !s.Current {
			t.Errorf("своя сессия не помечена")
		}
		if s.IP == "" {
			t.Errorf("нет IP: %+v", s)
		}
	}
	rec := e.post(e.owner, "/api/admin/sessions/close", fmt.Sprintf(`{"id":%q}`, foreign))
	if rec.Code != 200 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if rec := e.get(cookie, "/api/admin/me"); rec.Code != 401 {
		t.Errorf("закрытая сессия жива: %d", rec.Code)
	}
	if rec := e.post(e.owner, "/api/admin/sessions/close", `{"id":"00ff00ff00ff00ff"}`); rec.Code != 404 {
		t.Errorf("нет такой сессии: %d", rec.Code)
	}
}

func TestAdminAuditList(t *testing.T) {
	e := newAdminEnv(t)
	u := e.subscriber(2001, "ivan")
	if rec := e.post(e.owner, fmt.Sprintf("/api/admin/users/%d/days", u.ID), `{"days":30}`); rec.Code != 200 {
		t.Fatal(rec.Body)
	}
	rec := e.get(e.owner, "/api/admin/audit?action=sub.grant_days")
	golden(t, "admin_audit", rec.Body.Bytes())
	var list AdminAuditList
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil || len(list.Items) != 1 || list.Items[0].Details["days"] != "30" || list.Items[0].IP == "" {
		t.Fatalf("%+v %v", list, err)
	}
	if rec = e.get(e.owner, "/api/admin/audit?admin=abc"); rec.Code != 400 {
		t.Errorf("плохой админ: %d", rec.Code)
	}
	if rec = e.get(e.owner, "/api/admin/audit?from=2026-01-01&to=2026-01-01&action=sub.grant_days"); !strings.Contains(rec.Body.String(), `"days":"30"`) {
		t.Errorf("по датам день включается целиком: %s", rec.Body)
	}
	cookie := e.helper(75, core.PermStats)
	if rec = e.get(cookie, "/api/admin/audit"); rec.Code != 403 {
		t.Errorf("помощник открыл журнал: %d", rec.Code)
	}
}

// Пароль владельца задаётся только командой на сервере: адреса смены в API нет.
func TestNoOwnerPasswordRoute(t *testing.T) {
	e := newAdminEnv(t)
	for _, rt := range AdminRoutes() {
		if strings.Contains(strings.ToLower(rt.Pattern), "password") {
			t.Errorf("в таблице адрес про пароль: %s", rt.Pattern)
		}
	}
	for _, path := range []string{"/api/admin/team/password", "/api/admin/owner/password", "/api/admin/password", "/api/admin/team/owner"} {
		for _, method := range []string{"POST", "PUT", "PATCH"} {
			rec := e.do(e.owner, method, path, `{"password":"новый пароль 12345"}`)
			if rec.Code != 404 && rec.Code != 405 {
				t.Errorf("%s %s: %d", method, path, rec.Code)
			}
		}
	}
	// Владелец по-прежнему входит старым паролем.
	if _, err := e.c.LoginStart(context.Background(), adminOwner, ownerPass, core.LoginInfo{IP: "203.0.113.9"}); err != nil {
		t.Errorf("пароль владельца изменился: %v", err)
	}
}
