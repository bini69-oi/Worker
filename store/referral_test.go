package store

import (
	"context"
	"testing"

	"github.com/bini69-oi/worker/core"
)

func TestRefStatsOnSQLite(t *testing.T) {
	c, _ := sqlCore(t)
	ctx := context.Background()
	host, err := c.Register(ctx, 1, "host", "ru")
	if err != nil {
		t.Fatal(err)
	}
	tr, err := c.SaveTariff(ctx, core.Tariff{Name: "Месяц", Active: true, Periods: []core.Period{{Days: 30, Price: 10000}}})
	if err != nil {
		t.Fatal(err)
	}
	for i := range 3 {
		f, err := c.RegisterRef(ctx, int64(10+i), "f", "ru", host.RefCode)
		if err != nil {
			t.Fatal(err)
		}
		if i == 2 {
			continue
		}
		if _, err := c.AdminAdjust(ctx, 9, f.ID, 10000, "тест"); err != nil {
			t.Fatal(err)
		}
		if _, err := c.Buy(ctx, f.ID, tr.ID, 30); err != nil {
			t.Fatal(err)
		}
	}
	st, err := c.RefStats(ctx, host.ID)
	if err != nil {
		t.Fatal(err)
	}
	if st.Invited != 3 || st.Bought != 2 || st.Earned != 2000 {
		t.Fatalf("ждали 3, 2 и 2000: %+v", st)
	}
}
