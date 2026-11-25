package store

import (
	"context"
	"testing"

	"github.com/bini69-oi/worker/core"
	"github.com/bini69-oi/worker/core/coretest"
	"github.com/bini69-oi/worker/pay/manual"
)

func TestRefStatsOnSQLite(t *testing.T) {
	c, s := sqlCore(t)
	ctx := context.Background()
	cfg := coretest.NewConfig(t)
	for k, v := range map[string]string{"manual.enabled": "true", "manual.details": "Карта 0000"} {
		if err := cfg.Set(k, v); err != nil {
			t.Fatal(err)
		}
	}
	pc := core.New(core.Deps{Store: s, Clock: coretest.NewClock(tNow), Config: cfg, OwnerID: 1, Gateways: []core.Gateway{manual.New(cfg)}})
	host, err := c.Register(ctx, 1, "host", "ru")
	if err != nil {
		t.Fatal(err)
	}
	tr, err := c.SaveTariff(ctx, 1, core.Tariff{Name: "Месяц", Active: true, Periods: []core.Period{{Days: 30, Price: 10000}}})
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
		p, _, err := pc.CreatePayment(ctx, f.ID, "manual", 10000)
		if err != nil {
			t.Fatal(err)
		}
		if err := pc.ConfirmManual(ctx, 1, p.ID); err != nil {
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
