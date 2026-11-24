package e2e

import (
	"context"
	"runtime"
	"testing"

	"github.com/bini69-oi/worker/core"
)

// TestMemory10k кладёт 10 тысяч абонентов с подписками, прогоняет фоновые задачи и проверяет,
// что Go-память процесса держится до 50 МБ. Долгий тест, в коротком режиме (-short) пропускается.
func TestMemory10k(t *testing.T) {
	if testing.Short() {
		t.Skip("долгий тест")
	}
	w := newWorld(t)
	ctx := context.Background()
	tr, err := w.c.SaveTariff(ctx, core.Tariff{Name: "Месяц", Active: true, Periods: []core.Period{{Days: 30, Price: 0}}})
	if err != nil {
		t.Fatal(err)
	}
	const users = 10_000
	for i := range users {
		u, err := w.c.Register(ctx, int64(1_000_000+i), "u", "ru")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.c.Buy(ctx, u.ID, tr.ID, 30); err != nil {
			t.Fatal(err)
		}
	}
	for range 3 {
		for _, run := range []func(context.Context) (int, error){w.c.RemindDue, w.c.ExpireDue, w.c.ReconcilePayments} {
			if _, err := run(ctx); err != nil {
				t.Fatal(err)
			}
		}
	}

	runtime.GC()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	const limit = 50 << 20
	t.Logf("10 тысяч абонентов: куча %d МБ, всего у Go %d МБ", m.HeapInuse>>20, m.Sys>>20)
	if m.Sys > limit {
		t.Fatalf("память %d МБ, предел 50 МБ", m.Sys>>20)
	}
}
