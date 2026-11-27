package core_test

import (
	"slices"
	"testing"
)

func TestTopupPresets(t *testing.T) {
	e := newEnv(t)
	for in, want := range map[string][]int64{
		"100,250,500,1000":     {10000, 25000, 50000, 100000},
		" 100 , x, -5, 0, 300": {10000, 30000},
		"1000000,1000001":      {100000000},
		"":                     nil,
	} {
		if err := e.cfg.Set("pay.presets", in); err != nil {
			t.Fatal(err)
		}
		if got := e.c.TopupPresets(); !slices.Equal(got, want) {
			t.Errorf("%q: %v, ждали %v", in, got, want)
		}
	}
}
