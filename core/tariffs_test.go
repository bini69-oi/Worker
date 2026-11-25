package core_test

import (
	"context"
	"errors"
	"testing"

	"github.com/bini69-oi/worker/core"
)

func TestTariffValidation(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	bad := []core.Tariff{
		{Name: "", Periods: []core.Period{{Days: 30, Price: 100}}},
		{Name: "Минус", Periods: []core.Period{{Days: 30, Price: -1}}},
		{Name: "Без сроков"},
		{Name: "Ноль дней", Periods: []core.Period{{Days: 0, Price: 100}}},
		{Name: "Повтор", Periods: []core.Period{{Days: 30, Price: 1}, {Days: 30, Price: 2}}},
		{Name: "Сброс", Reset: "YEAR", Periods: []core.Period{{Days: 30, Price: 1}}},
	}
	for _, tr := range bad {
		if _, err := e.c.SaveTariff(ctx, owner, tr); !errors.Is(err, core.ErrInvalid) {
			t.Errorf("%q: ждали ErrInvalid, получили %v", tr.Name, err)
		}
	}
}

func TestTariffsOrderAndHidden(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	month := e.tariff(t, "Месяц", core.Period{Days: 30, Price: 15000})
	year := e.tariff(t, "Год", core.Period{Days: 365, Price: 150000})
	year.Sort = -1
	if _, err := e.c.SaveTariff(ctx, owner, year); err != nil {
		t.Fatal(err)
	}
	list, _ := e.c.Tariffs(ctx)
	if len(list) != 2 || list[0].Name != "Год" {
		t.Fatalf("ждали «Год» первым: %+v", list)
	}
	month.Active = false
	if _, err := e.c.SaveTariff(ctx, owner, month); err != nil {
		t.Fatal(err)
	}
	list, _ = e.c.Tariffs(ctx)
	if len(list) != 1 || list[0].Name != "Год" {
		t.Fatalf("скрытый тариф в продаже: %+v", list)
	}
}
