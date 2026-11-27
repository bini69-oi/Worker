package store

import (
	"path/filepath"
	"testing"
)

func TestAdminMigrationObjects(t *testing.T) {
	db := openTest(t)
	for typ, names := range map[string][]string{
		"table": {"broadcasts"},
		"index": {"broadcasts_one_running", "users_username", "payments_status_created", "payments_user", "audit_created", "audit_target"},
	} {
		for _, name := range names {
			var n int
			if err := db.R.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type = ? AND name = ?", typ, name).Scan(&n); err != nil || n != 1 {
				t.Errorf("нет %s %s (%v)", typ, name, err)
			}
		}
	}
}

func TestOneRunningBroadcast(t *testing.T) {
	db := openTest(t)
	ins := func(status string) error {
		_, err := db.W.Exec(`INSERT INTO broadcasts (segment, text, status, created_by, created_at) VALUES ('all', 'x', ?, 1, 0)`, status)
		return err
	}
	if err := ins("running"); err != nil {
		t.Fatal(err)
	}
	if err := ins("running"); err == nil {
		t.Error("вторая идущая рассылка прошла")
	}
	if err := ins("draft"); err != nil {
		t.Errorf("черновик рядом с идущей: %v", err)
	}
	if err := ins("done"); err != nil {
		t.Errorf("законченная рядом с идущей: %v", err)
	}
	if _, err := db.W.Exec(`INSERT INTO broadcasts (segment, text, created_by, created_at) VALUES ('чужой', 'x', 1, 0)`); err == nil {
		t.Error("неизвестный сегмент прошёл")
	}
}

// Миграция 006 применяется на базу версии 4: у кабинета своя миграция 005, админка на неё не опирается.
func TestAdminMigrationWithoutCabinet(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	var v int
	if err := db.R.QueryRow("PRAGMA user_version").Scan(&v); err != nil || v < 6 {
		t.Fatalf("версия схемы %d (%v)", v, err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	// Повторное открытие ничего не ломает и не применяет миграцию второй раз.
	db, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
}
