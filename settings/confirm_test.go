package settings

import "testing"

func TestIsMoneyAndNeedsConfirm(t *testing.T) {
	cases := []struct {
		key        string
		money, ask bool
	}{
		{"stars.rate", true, true},
		{"ref.first_percent", true, true},
		{"yookassa.secret_key", false, true},
		{"panel.webhook_secret", false, true},
		{"admin.allow_ips", false, true},
		{"trial.days", false, false},
		{"maintenance.on", false, false},
		{"connect.apps", false, false},
	}
	for _, c := range cases {
		if IsMoney(c.key) != c.money || NeedsConfirm(c.key) != c.ask {
			t.Errorf("%s: деньги %v, подтверждение %v", c.key, IsMoney(c.key), NeedsConfirm(c.key))
		}
	}
}

func TestValidateAndCurrent(t *testing.T) {
	s, _ := newSettings(t)
	if err := s.Validate("trial.days", "много"); err == nil {
		t.Error("число принято «много»")
	}
	if err := s.Validate("trial.days", "5"); err != nil {
		t.Error(err)
	}
	if err := s.Validate("нет.такой", "1"); err == nil {
		t.Error("неизвестный ключ")
	}
	if v, changed := s.Current("trial.days"); v != "3" || changed {
		t.Errorf("умолчание: %q %v", v, changed)
	}
	if err := s.Set("trial.days", "7"); err != nil {
		t.Fatal(err)
	}
	if v, changed := s.Current("trial.days"); v != "7" || !changed {
		t.Errorf("после правки: %q %v", v, changed)
	}
	if err := s.SetSecret("yookassa.secret_key", "live_0123456789abcdef"); err != nil {
		t.Fatal(err)
	}
	if v, changed := s.Current("yookassa.secret_key"); v != "" || !changed {
		t.Errorf("секрет не должен отдаваться: %q %v", v, changed)
	}
}

// Prepare проверяет и готовит запись, Apply применяет в памяти: пока Apply не позван, значение прежнее.
func TestPrepareApply(t *testing.T) {
	s, f := newSettings(t)
	ch, err := s.PrepareSet("trial.days", "9")
	if err != nil || ch.Delete || ch.Stored != "9" || ch.Key != "trial.days" {
		t.Fatalf("%+v %v", ch, err)
	}
	if s.Int("trial.days") != 3 || len(f.rows) != 0 {
		t.Fatal("Prepare изменил значение или базу")
	}
	s.Apply(ch)
	if s.Int("trial.days") != 9 {
		t.Error("Apply не применил")
	}
	if ch, _ := s.PrepareSet("trial.days", "3"); !ch.Delete {
		t.Error("значение по умолчанию это удаление строки")
	}
	if _, err := s.PrepareSet("trial.days", "много"); err == nil {
		t.Error("неверный тип принят")
	}
	if _, err := s.PrepareSet("yookassa.secret_key", "x"); err == nil {
		t.Error("секрет обычной правкой")
	}
	sec, err := s.PrepareSecret("yookassa.secret_key", "live_0123456789abcdefXYZ1")
	if err != nil || sec.Stored == "live_0123456789abcdefXYZ1" || !sec.Secret || sec.Value != "live_0123456789abcdefXYZ1" {
		t.Fatalf("секрет: %+v %v", sec, err)
	}
	s.Apply(sec)
	if s.Secret("yookassa.secret_key") != "live_0123456789abcdefXYZ1" {
		t.Error("секрет не применился")
	}
	rs, _ := s.PrepareReset("trial.days")
	s.Apply(rs)
	if s.Int("trial.days") != 3 {
		t.Error("сброс не применился")
	}
}
