package store

import (
	"testing"
	"time"

	"github.com/bini69-oi/worker/settings"
)

func newSettings(t *testing.T, db *DB) *settings.Settings {
	t.Helper()
	s, err := settings.New(NewSettingsStore(db), time.Now)
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
