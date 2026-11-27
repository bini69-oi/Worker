package settings

import (
	"errors"
	"strings"
	"testing"
)

func TestAddonsTrafficCheck(t *testing.T) {
	s, _ := newSettings(t)
	cases := []struct {
		name, value string
		ok          bool
	}{
		{"пусто: докупки нет", "", true},
		{"один пакет", "10:15000", true},
		{"два пакета с пробелом", "10:15000, 50:50000", true},
		{"единицы измерения в числе", "10ГБ:150", false},
		{"без цены", "10", false},
		{"без гигабайт", ":15000", false},
		{"нулевая цена", "10:0", false},
		{"отрицательные гигабайты", "-5:100", false},
		{"слишком много гигабайт", "100000:100", false},
		{"повтор пакета", "10:100,10:200", false},
		{"лишняя запятая", "10:100,", false},
		{"слишком много пакетов", "1:1,2:1,3:1,4:1,5:1,6:1,7:1,8:1,9:1,10:1,11:1", false},
		{"цена не число", "10:много", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := s.Validate("addons.traffic", c.value)
			if c.ok != (err == nil) {
				t.Fatalf("%q: %v", c.value, err)
			}
			if !c.ok {
				if !errors.Is(err, ErrBadValue) || !strings.Contains(err.Error(), "10:15000") {
					t.Errorf("в ошибке нет примера формата: %v", err)
				}
				if err := s.Set("addons.traffic", c.value); err == nil {
					t.Errorf("Set принял %q", c.value)
				}
			}
		})
	}
}

func TestAllowIPsCheck(t *testing.T) {
	s, _ := newSettings(t)
	for _, ok := range []string{"", "203.0.113.5", "203.0.113.5, 10.0.0.0/8", "2001:db8::1", "2001:db8::/32"} {
		if err := s.Validate("admin.allow_ips", ok); err != nil {
			t.Errorf("%q: %v", ok, err)
		}
	}
	for _, bad := range []string{"300.1.1.1", "ivan", "10.0.0.0/33", "1.1.1.1,", "1.1.1.1;2.2.2.2", "*"} {
		if err := s.Validate("admin.allow_ips", bad); !errors.Is(err, ErrBadValue) {
			t.Errorf("%q прошло: %v", bad, err)
		}
	}
	if d, ok := find("admin.allow_ips"); !ok || d.Default != "" || d.Secret {
		t.Errorf("настройка в реестре: %+v", d)
	}
}
