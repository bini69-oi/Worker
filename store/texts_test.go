package store

import (
	"testing"
	"time"

	"github.com/bini69-oi/worker/texts"
)

func newTexts(t *testing.T, db *DB) *texts.Texts {
	t.Helper()
	tx, err := texts.New(NewTextsStore(db), func() string { return "ru" }, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	return tx
}

func TestTextsSetResetAndRestart(t *testing.T) {
	db := openTest(t)
	tx := newTexts(t, db)
	if err := tx.Set("buy.success", "en", "Paid, {days} days"); err != nil {
		t.Fatal(err)
	}
	if err := tx.Set("buy.success", "en", "Paid for {days} days"); err != nil {
		t.Fatal(err)
	}
	if got := newTexts(t, db).Raw("buy.success", "en"); got != "Paid for {days} days" {
		t.Fatalf("после перезапуска получили %q", got)
	}
	if err := tx.Reset("buy.success", "en"); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := db.R.QueryRow("SELECT COUNT(*) FROM texts").Scan(&n); err != nil || n != 0 {
		t.Fatalf("после сброса строка должна исчезнуть (%d, %v)", n, err)
	}
}
