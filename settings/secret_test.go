package settings

import (
	"errors"
	"strings"
	"testing"
)

func TestSecretRoundTripAndRandomNonce(t *testing.T) {
	a, err := seal(testKey, "k", "значение")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := seal(testKey, "k", "значение")
	if a == b {
		t.Fatal("одно и то же значение должно шифроваться по-разному (случайный nonce)")
	}
	if !strings.HasPrefix(a, "enc1:") || strings.Contains(a, "значение") {
		t.Fatal("плохой формат шифротекста")
	}
	got, err := open(testKey, "k", a)
	if err != nil || got != "значение" {
		t.Fatalf("не расшифровалось: %v", err)
	}
}

func TestSecretTamperedOrOtherNameOrKey(t *testing.T) {
	enc, _ := seal(testKey, "yookassa.secret_key", "abc")
	other := []byte("ffffffffffffffffffffffffffffffff")
	if _, err := open(other, "yookassa.secret_key", enc); !errors.Is(err, errDecrypt) {
		t.Error("чужой ключ должен давать отказ")
	}
	if _, err := open(testKey, "другая.настройка", enc); !errors.Is(err, errDecrypt) {
		t.Error("перенос в другую настройку должен давать отказ")
	}
	if _, err := open(testKey, "yookassa.secret_key", enc[:len(enc)-4]); !errors.Is(err, errDecrypt) {
		t.Error("повреждённая строка должна давать отказ")
	}
	if _, err := open(testKey, "k", "просто текст"); !errors.Is(err, errDecrypt) {
		t.Error("строка без префикса должна давать отказ")
	}
}

func TestMask(t *testing.T) {
	cases := []struct {
		in   string
		want Masked
	}{
		{"", Masked{}},
		{"abc", Masked{IsSet: true}},
		{"live_1234567890", Masked{IsSet: true, Last4: "7890"}},
	}
	for _, c := range cases {
		if got := mask(c.in); got != c.want {
			t.Errorf("mask(%q) = %+v, ждали %+v", c.in, got, c.want)
		}
	}
}

func TestSecretSettingAPI(t *testing.T) {
	s, f := newSettings(t)
	if m := s.Masked("yookassa.secret_key"); m.IsSet {
		t.Fatal("по умолчанию секрет не задан")
	}
	const secret = "live_1234567890"
	if err := s.SetSecret("yookassa.secret_key", secret); err != nil {
		t.Fatal(err)
	}
	if m := s.Masked("yookassa.secret_key"); !m.IsSet || m.Last4 != "7890" {
		t.Fatalf("маска неверная: %+v", m)
	}
	if s.String("yookassa.secret_key") != "" {
		t.Fatal("String не должен отдавать секрет")
	}
	if strings.Contains(f.rows["yookassa.secret_key"], "1234567890") {
		t.Fatal("в хранилище лежит открытый секрет")
	}
	// Другой экземпляр на тех же данных (как после перезапуска) читает тот же секрет.
	s2, err := New(f, testKey, s.now)
	if err != nil || s2.Secret("yookassa.secret_key") != secret {
		t.Fatalf("после перезапуска секрет не прочитан: %v", err)
	}
	if err := s.SetSecret("yookassa.secret_key", ""); err != nil || f.has("yookassa.secret_key") {
		t.Fatal("пустое значение должно сбросить секрет")
	}
}

func TestSecretNotInErrors(t *testing.T) {
	f := newFake()
	f.rows["yookassa.secret_key"] = "enc1:мусор-live_1234567890"
	_, err := New(f, testKey, t0Func)
	if err == nil || strings.Contains(err.Error(), "1234567890") {
		t.Fatalf("ждали отказ без значения в тексте, получили %v", err)
	}
}

func TestBadKeyLength(t *testing.T) {
	if _, err := New(newFake(), []byte("короткий"), t0Func); err == nil {
		t.Fatal("ключ не в 32 байта должен отклоняться")
	}
}

func TestSecretNeedsConfirm(t *testing.T) {
	s, _ := newSettings(t)
	if err := s.Set("yookassa.secret_key", "live_key_value"); !errors.Is(err, ErrSecretNeedsConfirm) {
		t.Fatalf("секрет записан обычной правкой: %v", err)
	}
	if err := s.SetSecret("trial.days", "5"); err == nil {
		t.Fatal("SetSecret записал обычную настройку")
	}
}
