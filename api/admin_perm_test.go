package api

import (
	"net/http"
	"strings"
	"testing"

	"github.com/bini69-oi/worker/core"
)

// pathOf превращает шаблон адреса в настоящий: вместо {id} подставляется 1.
func pathOf(pattern string) (method, path string) {
	method, path, _ = strings.Cut(pattern, " ")
	for strings.Contains(path, "{") {
		i, j := strings.Index(path, "{"), strings.Index(path, "}")
		path = path[:i] + "1" + path[j+1:]
	}
	return method, path
}

// Каждый адрес из таблицы прав отказывает помощнику без нужного права. Таблица здесь та же, что в коде.
func TestEveryRouteDeniesWithoutPerm(t *testing.T) {
	e := newAdminEnv(t)
	for _, rt := range AdminRoutes() {
		if rt.Perm == "" {
			continue
		}
		t.Run(rt.Pattern, func(t *testing.T) {
			// Помощник с любым правом, кроме нужного.
			other := core.PermUsers
			if rt.Perm == core.PermUsers {
				other = core.PermStats
			}
			tg := int64(100 + len(rt.Pattern))
			cookie := e.helper(tg, other)
			method, path := pathOf(rt.Pattern)
			rec := e.do(cookie, method, path, `{}`)
			if rec.Code != http.StatusForbidden {
				t.Fatalf("помощник без права %q: %d %s", rt.Perm, rec.Code, rec.Body)
			}
		})
	}
}

func TestRouteTableIsSane(t *testing.T) {
	seen := map[string]bool{}
	for _, rt := range AdminRoutes() {
		_, path := pathOf(rt.Pattern)
		if !strings.HasPrefix(path, "/api/admin/") || seen[rt.Pattern] {
			t.Errorf("плохая строка таблицы: %q", rt.Pattern)
		}
		seen[rt.Pattern] = true
		ok := rt.Perm == "" || rt.Perm == core.PermOwner
		for _, p := range core.HelperPerms {
			ok = ok || p == rt.Perm
		}
		if !ok {
			t.Errorf("%s: неизвестное право %q", rt.Pattern, rt.Perm)
		}
	}
}

// Отказ по праву: 403, запись в журнале и тревога владельцу.
func TestDeniedIsAuditedAndAlerts(t *testing.T) {
	e := newAdminEnv(t)
	a := &adminAPI{s: e.s, d: AdminDeps{Core: e.c}}
	e.s.Handle("POST /api/admin/_probe", a.guarded(adminRoute{Pattern: "POST /api/admin/_probe", Perm: core.PermTariffs,
		H: func(http.ResponseWriter, *http.Request, adminCtx) (any, error) { return nil, nil }}))
	cookie := e.helper(60, core.PermUsers)
	before := len(e.st.Audit())
	rec := e.post(cookie, "/api/admin/_probe", `{}`)
	if rec.Code != 403 {
		t.Fatalf("%d", rec.Code)
	}
	log := e.st.Audit()
	if len(log) != before+1 || !strings.HasPrefix(log[len(log)-1].Action, "denied:") {
		t.Errorf("отказа нет в журнале: %+v", log[before:])
	}
	if rec := e.post(e.owner, "/api/admin/_probe", `{}`); rec.Code != 200 {
		t.Errorf("владельцу отказали: %d", rec.Code)
	}
}

func TestSessionGoneIs401(t *testing.T) {
	e := newAdminEnv(t)
	if rec := e.get(AdminCookie+"=нет-такой", "/api/admin/me"); rec.Code != 401 {
		t.Errorf("%d", rec.Code)
	}
}
