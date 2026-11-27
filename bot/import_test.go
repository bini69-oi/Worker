package bot

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/bini69-oi/worker/core"
)

// Перенесённая подписка показывает лимиты из панели, без автопродления и докупки.
func TestImportedSubScreen(t *testing.T) {
	e := newEnv(t)
	e.set(t, "addons.traffic", "10:15000", "addons.device_month", "10000")
	e.start(t)
	e.panel.AddListed(core.PanelEntry{Ref: "r1", Username: "old_one", TgID: userTg, Status: "ACTIVE",
		ExpireAt: t0.Add(45 * 24 * time.Hour), CreatedAt: t0.Add(-100 * 24 * time.Hour), SubURL: "https://sub.example/r1",
		TrafficBytes: 50 << 30, Devices: 3, Reset: "MONTH"})
	if _, err := e.c.ImportPanel(context.Background(), owner, true); err != nil {
		t.Fatal(err)
	}
	e.press(t, userTg, 5, "sub")
	got := e.api.take()
	golden(t, "sub_imported", got)
	if s := render(got); strings.Contains(s, "→ ar:") || strings.Contains(s, "→ at]") || strings.Contains(s, "→ ad]") {
		t.Fatalf("у перенесённой подписки автопродление или докупка: %s", s)
	}
}
