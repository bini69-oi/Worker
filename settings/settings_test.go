package settings

import (
	"errors"
	"testing"
	"time"
)

var testKey = []byte("0123456789abcdef0123456789abcdef")

var t0 = time.Unix(1_700_000_000, 0)

func t0Func() time.Time { return t0 }

func newSettings(t *testing.T) (*Settings, *fakeStore) {
	t.Helper()
	f := newFake()
	s, err := New(f, testKey, func() time.Time { return t0 })
	if err != nil {
		t.Fatal(err)
	}
	return s, f
}

func TestDefaultsParse(t *testing.T) {
	for _, d := range registry {
		if _, err := normalize(d.Kind, d.Default); err != nil {
			t.Errorf("умолчание %s не подходит к типу %s", d.Key, d.Kind)
		}
	}
}

func TestRegistryKeysUnique(t *testing.T) {
	seen := map[string]bool{}
	for _, d := range registry {
		if seen[d.Key] {
			t.Errorf("ключ %s повторён", d.Key)
		}
		seen[d.Key] = true
		if d.Desc == "" || d.Group == "" {
			t.Errorf("у %s нет описания или группы", d.Key)
		}
	}
}

func TestDefaultValue(t *testing.T) {
	s, _ := newSettings(t)
	if !s.Bool("trial.enabled") || s.Int("trial.days") != 3 || s.Money("stars.rate") != 150 {
		t.Fatal("умолчания из реестра не действуют")
	}
}

func TestSetChangesValue(t *testing.T) {
	s, f := newSettings(t)
	if err := s.Set("trial.days", "7"); err != nil {
		t.Fatal(err)
	}
	if s.Int("trial.days") != 7 || !f.has("trial.days") {
		t.Fatal("значение не записалось")
	}
}

func TestUnknownKey(t *testing.T) {
	s, f := newSettings(t)
	err := s.Set("нет.такой", "1")
	if !errors.Is(err, ErrUnknownKey) {
		t.Fatalf("ждали ErrUnknownKey, получили %v", err)
	}
	if len(f.rows) != 0 {
		t.Fatal("неизвестный ключ попал в базу")
	}
	if !errors.Is(s.Reset("нет.такой"), ErrUnknownKey) {
		t.Fatal("сброс неизвестного ключа должен отказать")
	}
}

func TestBadTypeKeepsOldValue(t *testing.T) {
	s, f := newSettings(t)
	if err := s.Set("trial.days", "7"); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"много", "", "1.5"} {
		if err := s.Set("trial.days", bad); !errors.Is(err, ErrBadValue) {
			t.Fatalf("значение %q: ждали ErrBadValue, получили %v", bad, err)
		}
	}
	if s.Int("trial.days") != 7 || f.rows["trial.days"] != "7" {
		t.Fatal("старое значение пропало")
	}
}

func TestBadTypesAllKinds(t *testing.T) {
	s, _ := newSettings(t)
	cases := map[string]string{
		"trial.enabled": "может быть",
		"stars.rate":    "-5",
		// Сверх предела денег: цена на дни и устройства переполнила бы int64.
		"addons.device_month": "10000000001",
	}
	for key, v := range cases {
		if err := s.Set(key, v); !errors.Is(err, ErrBadValue) {
			t.Errorf("%s=%q: ждали ErrBadValue, получили %v", key, v, err)
		}
	}
}

func TestResetDeletesRow(t *testing.T) {
	s, f := newSettings(t)
	if err := s.Set("trial.days", "7"); err != nil {
		t.Fatal(err)
	}
	if err := s.Reset("trial.days"); err != nil {
		t.Fatal(err)
	}
	if f.has("trial.days") || s.Int("trial.days") != 3 {
		t.Fatal("после сброса строка должна исчезнуть, а умолчание вернуться")
	}
}

func TestSetDefaultStoresNothing(t *testing.T) {
	s, f := newSettings(t)
	if err := s.Set("trial.days", "7"); err != nil {
		t.Fatal(err)
	}
	if err := s.Set("trial.days", "3"); err != nil {
		t.Fatal(err)
	}
	if f.has("trial.days") {
		t.Fatal("значение, равное умолчанию, не должно лежать в базе")
	}
}

func TestStoreFailKeepsCache(t *testing.T) {
	s, f := newSettings(t)
	f.fail = errFake
	if err := s.Set("trial.days", "9"); !errors.Is(err, errFake) {
		t.Fatalf("ждали сбой хранилища, получили %v", err)
	}
	if s.Int("trial.days") != 3 {
		t.Fatal("при сбое записи кэш не должен меняться")
	}
}

func TestDurationAndBoolGetters(t *testing.T) {
	s, f := newSettings(t)
	f.rows["trial.enabled"] = "false"
	if err := s.Reload(); err != nil {
		t.Fatal(err)
	}
	if s.Bool("trial.enabled") {
		t.Fatal("trial.enabled должен быть выключен")
	}
	if got, _ := normalize(KindDuration, "90m"); got != "1h30m0s" {
		t.Fatalf("длительность записана как %q", got)
	}
}

func TestReloadKeepsSecretThatStoppedDecrypting(t *testing.T) {
	st := newFake()
	s, err := New(st, NewKey(), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetSecret("yookassa.secret_key", "live_kassa_key_123"); err != nil {
		t.Fatal(err)
	}
	st.rows["yookassa.secret_key"] = "enc1:испорчено"
	if err := s.Reload(); err == nil {
		t.Fatal("испорченный секрет прочитан без ошибки")
	}
	if got := s.Secret("yookassa.secret_key"); got != "live_kassa_key_123" {
		t.Fatalf("после сбоя перечитывания секрет пропал: %q", got)
	}
}
