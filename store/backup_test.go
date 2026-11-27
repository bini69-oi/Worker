package store

import (
	"os"
	"path/filepath"
	"testing"
)

func TestVacuumIntoAndCheckFile(t *testing.T) {
	db := openTest(t)
	if _, err := db.W.Exec(`INSERT INTO settings(key, value, updated_at) VALUES ('probe', 'v', 1)`); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(t.TempDir(), "copy.db")
	if err := db.VacuumInto(t.Context(), dst); err != nil {
		t.Fatal(err)
	}
	if err := CheckFile(dst); err != nil {
		t.Fatalf("копия не прошла проверку: %v", err)
	}
	// Копия не пускает в базу ничего чужого: проверка открывает её только на чтение.
	if err := db.VacuumInto(t.Context(), dst); err == nil {
		t.Fatal("копия поверх существующего файла должна давать ошибку")
	}
}

func TestCheckFileRejects(t *testing.T) {
	dir := t.TempDir()
	garbage := filepath.Join(dir, "garbage.db")
	if err := os.WriteFile(garbage, []byte("это не база данных, просто текст на несколько сотен байт"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := CheckFile(garbage); err == nil {
		t.Error("мусор принят за базу")
	}
	if err := CheckFile(filepath.Join(dir, "нет.db")); err == nil {
		t.Error("несуществующий файл принят")
	}
	// Корректная SQLite без наших миграций: не база Worker.
	other := rawDB(t)
	_, _ = other.Exec(`CREATE TABLE x(a)`)
	var path string
	_ = other.QueryRow(`SELECT file FROM pragma_database_list WHERE name='main'`).Scan(&path)
	if err := CheckFile(path); err == nil {
		t.Error("чужая база без миграций принята")
	}
}
