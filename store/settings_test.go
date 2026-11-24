package store

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bini69-oi/worker/settings"
)

func newSettings(t *testing.T, db *DB) *settings.Settings {
	t.Helper()
	s, err := settings.New(NewSettingsStore(db), testKey, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestSettingsOnlyDifferencesInDB(t *testing.T) {
	db := openTest(t)
	s := newSettings(t, db)
	count := func() int {
		var n int
		if err := db.R.QueryRow("SELECT COUNT(*) FROM settings").Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if count() != 0 {
		t.Fatal("чистая база не должна хранить настройки")
	}
	if err := s.Set("trial.days", "7"); err != nil {
		t.Fatal(err)
	}
	if count() != 1 {
		t.Fatal("ждали одну строку после правки")
	}
	if err := s.Reset("trial.days"); err != nil {
		t.Fatal(err)
	}
	if count() != 0 {
		t.Fatal("после сброса строка должна исчезнуть")
	}
}

func TestSettingsSurviveRestart(t *testing.T) {
	db := openTest(t)
	if err := newSettings(t, db).Set("trial.days", "9"); err != nil {
		t.Fatal(err)
	}
	if got := newSettings(t, db).Int("trial.days"); got != 9 {
		t.Fatalf("после перезапуска ждали 9, получили %d", got)
	}
}

var testKey = []byte("0123456789abcdef0123456789abcdef")

// В файлах базы (основной, журнал WAL) не должно быть открытого ключа кассы.
func TestSecretNotPlainInDBFiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secret.db")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	s := newSettings(t, db)
	const secret = "live_SUPER-SECRET-KEY-12345"
	if err := s.SetSecret("yookassa.secret_key", secret); err != nil {
		t.Fatal(err)
	}
	if got := s.Secret("yookassa.secret_key"); got != secret {
		t.Fatal("секрет не прочитался обратно")
	}
	// Один раз читаем то, что реально лежит в базе: строка есть и она зашифрована.
	var stored string
	if err := db.R.QueryRow("SELECT value FROM settings WHERE key = 'yookassa.secret_key'").Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(stored, "enc1:") {
		t.Fatalf("в базе ждали строку с префиксом enc1:, получили другую (длина %d)", len(stored))
	}
	files, err := filepath.Glob(path + "*")
	if err != nil || len(files) == 0 {
		t.Fatalf("файлы базы не найдены (%v)", err)
	}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(data, []byte(secret)) || bytes.Contains(data, []byte("SUPER-SECRET")) {
			t.Errorf("в файле %s лежит открытый секрет", filepath.Base(f))
		}
	}
}

func TestSecretWrongKeyRefusesStart(t *testing.T) {
	db := openTest(t)
	if err := newSettings(t, db).SetSecret("yookassa.secret_key", "live_abcdefghij"); err != nil {
		t.Fatal(err)
	}
	other := []byte("ffffffffffffffffffffffffffffffff")
	_, err := settings.New(NewSettingsStore(db), other, time.Now)
	if err == nil {
		t.Fatal("с чужим ключом шифрования настройки не должны открываться")
	}
	if strings.Contains(err.Error(), "live_abcdefghij") {
		t.Fatal("секрет попал в текст ошибки")
	}
}
