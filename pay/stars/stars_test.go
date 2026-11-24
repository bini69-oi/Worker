package stars

import (
	"context"
	"testing"

	"github.com/bini69-oi/worker/core"
	"github.com/bini69-oi/worker/core/coretest"
	"github.com/bini69-oi/worker/pay/gatewaytest"
)

func TestStarsSuite(t *testing.T) {
	off := coretest.NewConfig(t)
	if err := off.Set("stars.rate", "0"); err != nil {
		t.Fatal(err)
	}
	gatewaytest.Run(t, gatewaytest.Case{
		Gateway:      New(coretest.NewConfig(t)),
		Unconfigured: New(off),
		Amount:       30000,
	})
}

func TestRateFromSettings(t *testing.T) {
	cfg := coretest.NewConfig(t)
	if err := cfg.Set("stars.rate", "180"); err != nil {
		t.Fatal(err)
	}
	inv, err := New(cfg).Create(context.Background(), core.Payment{Amount: 30000})
	if err != nil {
		t.Fatal(err)
	}
	if inv.ExtAmount != 167 || inv.ExtCurrency != "XTR" {
		t.Fatalf("30000 копеек по 180 за звезду: ждали 167 XTR, получили %d %s", inv.ExtAmount, inv.ExtCurrency)
	}
	cases := map[[2]int64]int64{{150, 150}: 1, {151, 150}: 2, {0, 150}: 0, {100, 0}: 0}
	for in, want := range cases {
		if got := Stars(in[0], in[1]); got != want {
			t.Errorf("Stars(%d, %d) = %d, ждали %d", in[0], in[1], got, want)
		}
	}
}
