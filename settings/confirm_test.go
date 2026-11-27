package settings

import "testing"

func TestIsMoneyAndNeedsConfirm(t *testing.T) {
	cases := []struct {
		key        string
		money, ask bool
	}{
		{"manual.details", true, true},
		{"stars.rate", true, true},
		{"ref.first_percent", true, true},
		{"yookassa.return_url", true, true},
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
