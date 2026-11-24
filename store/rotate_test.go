package store

import (
	"strings"
	"testing"
	"time"

	"github.com/bini69-oi/worker/settings"
)

func secretSettings(t *testing.T, st *SettingsStore, key []byte) *settings.Settings {
	t.Helper()
	s, err := settings.New(st, key, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestRotateKey(t *testing.T) {
	st := NewSettingsStore(openTest(t))
	oldKey := settings.NewKey()
	s := secretSettings(t, st, oldKey)
	for k, v := range map[string]string{"yookassa.secret_key": "live_key_one", "panel.webhook_secret": "hook-secret-two", "trial.days": "7"} {
		set := s.Set
		if s.IsSecret(k) {
			set = s.SetSecret
		}
		if err := set(k, v); err != nil {
			t.Fatal(err)
		}
	}
	newKey := settings.NewKey()
	if err := st.Rotate(func(rows map[string]string) (map[string]string, error) {
		return settings.Reencrypt(rows, oldKey, newKey)
	}); err != nil {
		t.Fatal(err)
	}
	fresh := secretSettings(t, st, newKey)
	if fresh.Secret("yookassa.secret_key") != "live_key_one" || fresh.Secret("panel.webhook_secret") != "hook-secret-two" || fresh.Int("trial.days") != 7 {
		t.Fatal("после смены ключа секреты не читаются новым ключом")
	}
	if _, err := settings.New(st, oldKey, time.Now); err == nil {
		t.Fatal("старый ключ всё ещё читает секреты")
	}
}

func TestRotateFailsWhole(t *testing.T) {
	db := openTest(t)
	st := NewSettingsStore(db)
	oldKey := settings.NewKey()
	s := secretSettings(t, st, oldKey)
	for k, v := range map[string]string{"yookassa.secret_key": "live_key_one", "panel.webhook_secret": "hook-secret-two"} {
		set := s.Set
		if s.IsSecret(k) {
			set = s.SetSecret
		}
		if err := set(k, v); err != nil {
			t.Fatal(err)
		}
	}
	// Сбой посередине: запись второго секрета падает.
	if _, err := db.W.Exec(`CREATE TRIGGER fail_mid BEFORE UPDATE ON settings WHEN NEW.key = 'yookassa.secret_key'
		BEGIN SELECT RAISE(ABORT, 'сбой'); END`); err != nil {
		t.Fatal(err)
	}
	newKey := settings.NewKey()
	err := st.Rotate(func(rows map[string]string) (map[string]string, error) {
		return settings.Reencrypt(rows, oldKey, newKey)
	})
	if err == nil || !strings.Contains(err.Error(), "сбой") {
		t.Fatalf("ждали сбой, получили %v", err)
	}
	old := secretSettings(t, st, oldKey)
	if old.Secret("yookassa.secret_key") != "live_key_one" || old.Secret("panel.webhook_secret") != "hook-secret-two" {
		t.Fatal("после сбоя база не осталась целиком на старом ключе")
	}
}

func TestRotateWrongOldKey(t *testing.T) {
	st := NewSettingsStore(openTest(t))
	oldKey := settings.NewKey()
	if err := secretSettings(t, st, oldKey).SetSecret("yookassa.secret_key", "live_key_one"); err != nil {
		t.Fatal(err)
	}
	err := st.Rotate(func(rows map[string]string) (map[string]string, error) {
		return settings.Reencrypt(rows, settings.NewKey(), settings.NewKey())
	})
	if err == nil {
		t.Fatal("чужой старый ключ должен давать отказ")
	}
	if secretSettings(t, st, oldKey).Secret("yookassa.secret_key") != "live_key_one" {
		t.Fatal("отказ испортил секрет")
	}
}
