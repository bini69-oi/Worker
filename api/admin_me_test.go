package api

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/bini69-oi/worker/core"
)

func TestAdminMeGolden(t *testing.T) {
	e := newAdminEnv(t)
	rec := e.get(e.owner, "/api/admin/me")
	if rec.Code != 200 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	golden(t, "admin_me_owner", rec.Body.Bytes())

	cookie := e.helper(70, core.PermUsers, core.PermSupport)
	rec = e.get(cookie, "/api/admin/me")
	golden(t, "admin_me_helper", rec.Body.Bytes())
	if !strings.Contains(rec.Body.String(), `"owner":false`) {
		t.Errorf("помощник не владелец: %s", rec.Body)
	}
}

func TestAdminLogout(t *testing.T) {
	e := newAdminEnv(t)
	rec := e.post(e.owner, "/api/admin/logout", `{}`)
	if rec.Code != 200 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	cookies := (&http.Response{Header: rec.Header()}).Cookies()
	if len(cookies) != 1 || cookies[0].MaxAge >= 0 || cookies[0].Path != "/api/admin" {
		t.Errorf("cookie не сброшена: %v", rec.Header())
	}
	if rec := e.get(e.owner, "/api/admin/me"); rec.Code != 401 {
		t.Errorf("сессия жива после выхода: %d", rec.Code)
	}
}

func TestHelperRemovedLosesSession(t *testing.T) {
	e := newAdminEnv(t)
	cookie := e.helper(71, core.PermUsers)
	if rec := e.get(cookie, "/api/admin/me"); rec.Code != 200 {
		t.Fatalf("до удаления: %d", rec.Code)
	}
	ctx := context.Background()
	id, err := e.c.RequestRemoveHelper(ctx, adminOwner, 71)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.c.AnswerConfirm(ctx, id, adminOwner, true); err != nil {
		t.Fatal(err)
	}
	if rec := e.get(cookie, "/api/admin/me"); rec.Code != 401 {
		t.Errorf("после удаления помощника: %d", rec.Code)
	}
}
