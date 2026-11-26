package pay

import "testing"

func TestKopecks(t *testing.T) {
	good := map[string]int64{"300": 30000, "300.5": 30050, "300.50": 30050, "300.000000": 30000, " 1.01 ": 101, "0.01": 1,
		"999999999999.99": 99999999999999}
	for in, want := range good {
		if got, err := Kopecks(in); err != nil || got != want {
			t.Fatalf("%q: %d %v, ждали %d", in, got, err, want)
		}
	}
	for _, in := range []string{"", ".", "-1", "+1", "1e3", "300.505", "1,50", "1 000", "0x10", "1.-5", "9999999999999", "NaN", "300."} {
		if got, err := Kopecks(in); err == nil {
			t.Fatalf("%q принято как %d", in, got)
		}
	}
}

func FuzzKopecks(f *testing.F) {
	for _, s := range []string{"300.50", "-1", "1.-5", "999999999999.99"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		k, err := Kopecks(s)
		if err == nil && (k < 0 || k > 99999999999999) {
			t.Fatalf("%q дало %d", s, k)
		}
	})
}

func TestRubles(t *testing.T) {
	for k, want := range map[int64]string{30050: "300.50", 30000: "300.00", 1: "0.01", 0: "0.00"} {
		if got := Rubles(k); got != want {
			t.Fatalf("%d: %s", k, got)
		}
	}
}
