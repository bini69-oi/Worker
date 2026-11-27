package api

import (
	"strings"
	"testing"
	"time"

	"github.com/bini69-oi/worker/core"
)

func (e *adminEnv) listed(ref, username string, tg int64) {
	e.panel.AddListed(core.PanelEntry{Ref: ref, Username: username, TgID: tg, Status: "ACTIVE", ExpireAt: adminT0.AddDate(0, 2, 0),
		CreatedAt: adminT0.Add(-100 * 24 * time.Hour), SubURL: "https://sub.example/" + ref, TrafficBytes: 50 << 30, Devices: 3, Reset: "MONTH"})
}

func TestAdminImportPreviewAndApply(t *testing.T) {
	e := newAdminEnv(t)
	e.listed("r1", "old_one", 2001)
	e.panel.AddListed(core.PanelEntry{Ref: "r2", Username: "no_tg", Status: "ACTIVE", ExpireAt: adminT0.AddDate(0, 1, 0)})
	rec := e.post(e.owner, "/api/admin/import/preview", `{}`)
	if rec.Code != 200 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	golden(t, "admin_import_preview", rec.Body.Bytes())
	if _, err := e.c.UserByTg(contextBG, 2001); err == nil {
		t.Fatal("пробный прогон завёл абонента")
	}

	rec = e.post(e.owner, "/api/admin/import/apply", `{}`)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"pending":true`) {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if _, err := e.c.UserByTg(contextBG, 2001); err == nil {
		t.Fatal("абонент заведён до «Да»")
	}
	e.confirmLast(true)
	if _, err := e.c.UserByTg(contextBG, 2001); err != nil {
		t.Fatalf("после «Да»: %v", err)
	}
}

// Помощник нажал импорт: 403, подтверждения нет.
func TestAdminImportHelperDenied(t *testing.T) {
	e := newAdminEnv(t)
	e.listed("r1", "old_one", 2001)
	h := e.helper(91, core.PermUsers, core.PermGrant, core.PermStats)
	for _, path := range []string{"/api/admin/import/preview", "/api/admin/import/apply"} {
		if rec := e.post(h, path, `{}`); rec.Code != 403 {
			t.Errorf("%s: %d", path, rec.Code)
		}
	}
	if _, err := e.c.UserByTg(contextBG, 2001); err == nil {
		t.Error("абонент заведён помощником")
	}
}
