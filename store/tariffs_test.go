package store

import (
	"context"
	"testing"

	"github.com/bini69-oi/worker/core"
)

func TestTariffRoundTrip(t *testing.T) {
	s, _ := newCoreStore(t)
	ctx := context.Background()
	in := core.Tariff{
		Name: "Месяц", TrafficBytes: 100 << 30, Devices: 3, Reset: "MONTH", Squads: []string{"a", "b"},
		Active: true, Sort: 2, CreatedAt: tNow,
		Periods: []core.Period{{Days: 365, Price: 150000}, {Days: 30, Price: 15000}},
	}
	tx(t, s, func(tx core.Tx) error {
		var err error
		in.ID, err = tx.SaveTariff(in)
		return err
	})
	hidden := core.Tariff{Name: "Скрытый", Reset: "NO_RESET", Sort: 1, CreatedAt: tNow, Periods: []core.Period{{Days: 7, Price: 0}}}
	tx(t, s, func(tx core.Tx) error { _, err := tx.SaveTariff(hidden); return err })

	all, err := s.Tariffs(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 || all[0].Name != "Скрытый" {
		t.Fatalf("порядок по sort не соблюдён: %+v", all)
	}
	active, _ := s.Tariffs(ctx, true)
	if len(active) != 1 {
		t.Fatalf("ждали один активный тариф: %+v", active)
	}
	got := active[0]
	if got.TrafficBytes != in.TrafficBytes || got.Devices != 3 || got.Reset != "MONTH" || len(got.Squads) != 2 ||
		len(got.Periods) != 2 || got.Periods[0].Days != 30 || !got.CreatedAt.Equal(tNow) {
		t.Fatalf("тариф прочитан не так: %+v", got)
	}

	in.Periods = []core.Period{{Days: 30, Price: 20000}}
	tx(t, s, func(tx core.Tx) error { _, err := tx.SaveTariff(in); return err })
	tx(t, s, func(tx core.Tx) error {
		tr, err := tx.Tariff(in.ID)
		if err == nil && (len(tr.Periods) != 1 || tr.Periods[0].Price != 20000) {
			t.Errorf("периоды не заменились: %+v", tr.Periods)
		}
		return err
	})
}
