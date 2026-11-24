package store

import (
	"database/sql"
	"path/filepath"
	"sync"
	"testing"
	"testing/fstest"
)

func openTest(t *testing.T) *DB {
	t.Helper()
	db, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// rawDB открывает пустую базу без встроенных миграций, чтобы проверять сам механизм миграций.
func rawDB(t *testing.T) *sql.DB {
	t.Helper()
	w, err := sql.Open("sqlite", dsn(filepath.Join(t.TempDir(), "raw.db"), false))
	if err != nil {
		t.Fatal(err)
	}
	w.SetMaxOpenConns(1)
	t.Cleanup(func() { w.Close() })
	return w
}

func TestParallelWritesNoLock(t *testing.T) {
	db := openTest(t)
	const writers = 50

	var wg sync.WaitGroup
	errs := make(chan error, writers*2)
	for i := range writers {
		wg.Add(2)
		go func() {
			defer wg.Done()
			tx, err := db.W.Begin()
			if err != nil {
				errs <- err
				return
			}
			defer tx.Rollback()
			if _, err := tx.Exec("INSERT INTO users (tg_id, ref_code, created_at) VALUES (?, ?, 0)", i+1, i+1); err != nil {
				errs <- err
				return
			}
			errs <- tx.Commit()
		}()
		go func() {
			defer wg.Done()
			var n int
			errs <- db.R.QueryRow("SELECT COUNT(*) FROM users").Scan(&n)
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}

	var n int
	if err := db.R.QueryRow("SELECT COUNT(*) FROM users").Scan(&n); err != nil || n != writers {
		t.Fatalf("ждали %d абонентов, получили %d (%v)", writers, n, err)
	}
}

func TestReadPoolCannotWrite(t *testing.T) {
	db := openTest(t)
	if _, err := db.R.Exec("INSERT INTO users (tg_id, ref_code, created_at) VALUES (1, 'a', 0)"); err == nil {
		t.Fatal("пул чтения не должен писать")
	}
}

func TestMigrationsOnceInOrder(t *testing.T) {
	w := rawDB(t)
	fsys := fstest.MapFS{
		"migrations/002_b.sql":  {Data: []byte("INSERT INTO log (step) VALUES ('b');")},
		"migrations/001_a.sql":  {Data: []byte("CREATE TABLE log (step TEXT); INSERT INTO log (step) VALUES ('a');")},
		"migrations/010_c.sql":  {Data: []byte("INSERT INTO log (step) VALUES ('c');")},
		"migrations/readme.txt": {Data: []byte("не миграция")},
	}
	for range 2 {
		if err := migrate(w, fsys); err != nil {
			t.Fatal(err)
		}
	}

	rows, err := w.Query("SELECT step FROM log ORDER BY rowid")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var got string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			t.Fatal(err)
		}
		got += s
	}
	if got != "abc" {
		t.Fatalf("ждали шаги abc один раз, получили %q", got)
	}

	var v int
	if err := w.QueryRow("PRAGMA user_version").Scan(&v); err != nil || v != 10 {
		t.Fatalf("ждали user_version 10, получили %d (%v)", v, err)
	}
}

func TestMigrationDuplicateNumber(t *testing.T) {
	fsys := fstest.MapFS{
		"migrations/001_a.sql": {Data: []byte("SELECT 1;")},
		"migrations/001_b.sql": {Data: []byte("SELECT 1;")},
	}
	if _, err := migrationFiles(fsys); err == nil {
		t.Fatal("ждали ошибку про повтор номера")
	}
}

func TestMigrationFailRollsBack(t *testing.T) {
	w := rawDB(t)
	fsys := fstest.MapFS{
		"migrations/001_bad.sql": {Data: []byte("CREATE TABLE half (x INTEGER); SELECT * FROM нет_такой;")},
	}
	if err := migrate(w, fsys); err == nil {
		t.Fatal("ждали ошибку миграции")
	}
	var n int
	if err := w.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE name = 'half'").Scan(&n); err != nil || n != 0 {
		t.Fatalf("половина миграции осталась в базе (%v)", err)
	}
}

func TestInitHasAllTables(t *testing.T) {
	db := openTest(t)
	want := []string{
		"users", "tariffs", "tariff_periods", "subscriptions", "ledger", "payments", "outbox",
		"promo_codes", "promo_uses", "pending_buys", "settings", "texts", "notices",
		"admins", "admin_sessions", "confirms", "login_fails", "audit_log", "admin_chat",
	}
	for _, name := range want {
		var n int
		err := db.R.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?", name).Scan(&n)
		if err != nil || n != 1 {
			t.Errorf("нет таблицы %s (%v)", name, err)
		}
	}
}

func TestAuditLogInsertOnly(t *testing.T) {
	db := openTest(t)
	if _, err := db.W.Exec("INSERT INTO audit_log (admin_tg_id, action, created_at) VALUES (1, 'grant', 0)"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.W.Exec("UPDATE audit_log SET action = 'x'"); err == nil {
		t.Error("правка журнала действий должна быть запрещена")
	}
	if _, err := db.W.Exec("DELETE FROM audit_log"); err == nil {
		t.Error("удаление из журнала действий должно быть запрещено")
	}
}

func TestBalanceCannotGoNegative(t *testing.T) {
	db := openTest(t)
	_, err := db.W.Exec("INSERT INTO users (tg_id, ref_code, balance, created_at) VALUES (1, 'a', -1, 0)")
	if err == nil {
		t.Fatal("отрицательный баланс должен быть запрещён схемой")
	}
}
