package backup

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bini69-oi/worker/settings"
	"github.com/bini69-oi/worker/store"
)

const marker = "СЕКРЕТНАЯ-СТРОКА-ИЗ-БАЗЫ-7731"

var t0 = time.Date(2026, 11, 1, 3, 0, 0, 0, time.UTC)

func newService(t *testing.T, now *time.Time) (*Service, *store.DB) {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "w.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.W.Exec(`INSERT INTO settings(key, value, updated_at) VALUES ('probe', ?, 1)`, marker); err != nil {
		t.Fatal(err)
	}
	return &Service{
		DB: db, Secret: settings.NewKey(), Dir: filepath.Join(t.TempDir(), "backups"),
		Keep: func() int { return 14 }, Now: func() time.Time { return *now },
	}, db
}

func TestCreateEncrypted(t *testing.T) {
	now := t0
	s, _ := newService(t, &now)
	path, err := s.Create(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(path) != "worker-20261101-030000.db.enc" {
		t.Errorf("имя %q", filepath.Base(path))
	}
	if st, _ := os.Stat(path); st.Mode().Perm() != 0o600 {
		t.Errorf("права файла %v", st.Mode())
	}
	if st, _ := os.Stat(s.Dir); st.Mode().Perm() != 0o700 {
		t.Errorf("права папки %v", st.Mode())
	}
	raw, _ := os.ReadFile(path)
	if bytes.Contains(raw, []byte(marker)) || bytes.Contains(raw, []byte("SQLite format 3")) || bytes.Contains(raw, []byte("CREATE TABLE")) {
		t.Fatal("в бэкапе есть база открытым текстом")
	}
	// Во временной папке ничего не осталось.
	entries, _ := os.ReadDir(s.Dir)
	if len(entries) != 1 {
		t.Errorf("в папке бэкапов лишнее: %v", entries)
	}
}

func TestOpenNeedsKey(t *testing.T) {
	now := t0
	s, _ := newService(t, &now)
	path, _ := s.Create(t.Context())
	dst := filepath.Join(t.TempDir(), "out.db")
	if err := Decrypt(path, settings.NewKey(), dst); err == nil {
		t.Fatal("чужим ключом бэкап открылся")
	}
	if _, err := os.Stat(dst); err == nil {
		t.Fatal("при неверном ключе остался файл")
	}
	if err := Decrypt(path, s.Secret, dst); err != nil {
		t.Fatal(err)
	}
	if err := store.CheckFile(dst); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(dst)
	if !strings.Contains(string(b), marker) {
		t.Fatal("в расшифрованной базе нет данных")
	}
}

func TestCreateSameSecondRefuses(t *testing.T) {
	now := t0
	s, _ := newService(t, &now)
	if _, err := s.Create(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Create(t.Context()); err == nil {
		t.Fatal("второй бэкап в ту же секунду затёр первый")
	}
}

func list(t *testing.T, dir string) []string {
	t.Helper()
	es, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range es {
		out = append(out, e.Name())
	}
	return out
}

func TestKeepsLast(t *testing.T) {
	now := t0
	s, _ := newService(t, &now)
	s.Keep = func() int { return 14 }
	for range 15 {
		if _, err := s.Create(t.Context()); err != nil {
			t.Fatal(err)
		}
		if err := s.Prune(); err != nil {
			t.Fatal(err)
		}
		now = now.Add(24 * time.Hour)
	}
	got := list(t, s.Dir)
	if len(got) != 14 {
		t.Fatalf("осталось %d, ждали 14", len(got))
	}
	if got[0] != "worker-20261102-030000.db.enc" {
		t.Errorf("самый старый не удалён, первый %s", got[0])
	}
}

func TestPruneKeepsForeignFiles(t *testing.T) {
	now := t0
	s, _ := newService(t, &now)
	s.Keep = func() int { return 1 }
	_ = os.MkdirAll(s.Dir, 0o700)
	_ = os.WriteFile(filepath.Join(s.Dir, "заметка.txt"), []byte("x"), 0o600)
	for range 3 {
		_, _ = s.Create(t.Context())
		now = now.Add(time.Hour)
	}
	if err := s.Prune(); err != nil {
		t.Fatal(err)
	}
	if got := list(t, s.Dir); len(got) != 2 {
		t.Fatalf("ждали один бэкап и чужой файл, есть %v", got)
	}
}

func TestRunDueOncePerDay(t *testing.T) {
	now := t0
	s, _ := newService(t, &now)
	run := func() {
		t.Helper()
		if err := s.RunDue(t.Context()); err != nil {
			t.Fatal(err)
		}
	}

	run()
	if n := len(list(t, s.Dir)); n != 1 {
		t.Fatalf("первый запуск: %d файлов", n)
	}
	now = now.Add(23 * time.Hour)
	run()
	if n := len(list(t, s.Dir)); n != 1 {
		t.Fatalf("через 23 часа бэкап не нужен, файлов %d", n)
	}
	now = now.Add(time.Hour)
	run()
	if n := len(list(t, s.Dir)); n != 2 {
		t.Fatalf("через сутки нужен второй, файлов %d", n)
	}
}

func TestRunDueRespectsKeep(t *testing.T) {
	now := t0
	s, _ := newService(t, &now)
	s.Keep = func() int { return 2 }
	for range 5 {
		if err := s.RunDue(t.Context()); err != nil {
			t.Fatal(err)
		}
		now = now.Add(24 * time.Hour)
	}
	if n := len(list(t, s.Dir)); n != 2 {
		t.Fatalf("осталось %d, ждали 2", n)
	}
}

func TestCheck(t *testing.T) {
	now := t0
	s, _ := newService(t, &now)
	path, _ := s.Create(t.Context())
	if err := Check(path, s.Secret); err != nil {
		t.Fatalf("целый бэкап не прошёл: %v", err)
	}
	if err := Check(path, settings.NewKey()); err == nil {
		t.Error("проверка с чужим ключом прошла")
	}
	raw, _ := os.ReadFile(path)
	raw[len(raw)/2] ^= 0xff
	bad := filepath.Join(t.TempDir(), "worker-bad.db.enc")
	_ = os.WriteFile(bad, raw, 0o600)
	if err := Check(bad, s.Secret); err == nil {
		t.Error("испорченный бэкап прошёл проверку")
	}
	short := filepath.Join(t.TempDir(), "short.db.enc")
	_ = os.WriteFile(short, []byte("WBK1"), 0o600)
	if err := Check(short, s.Secret); err == nil {
		t.Error("обрезанный файл прошёл проверку")
	}
}

func TestRestore(t *testing.T) {
	now := t0
	s, db := newService(t, &now)
	path, _ := s.Create(t.Context())
	dbPath := dbFile(t, db)
	db.Close()

	// После бэкапа база изменилась: восстановление возвращает старое состояние, текущую базу откладывает.
	other, _ := store.Open(dbPath)
	_, _ = other.W.Exec(`UPDATE settings SET value='изменено' WHERE key='probe'`)
	other.Close()

	saved, err := Restore(dbPath, path, s.Secret, now)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(filepath.Base(saved), "w.db.before-restore-") {
		t.Errorf("копия текущей базы названа %q", saved)
	}
	restored, _ := store.Open(dbPath)
	defer restored.Close()
	var v string
	_ = restored.R.QueryRow(`SELECT value FROM settings WHERE key='probe'`).Scan(&v)
	if v != marker {
		t.Errorf("после восстановления %q", v)
	}
	old, _ := os.ReadFile(saved)
	if !bytes.Contains(old, []byte("изменено")) {
		t.Error("отложенная копия не содержит прежнего состояния")
	}
}

func TestRestoreBadFileKeepsDB(t *testing.T) {
	now := t0
	s, db := newService(t, &now)
	path, _ := s.Create(t.Context())
	dbPath := dbFile(t, db)
	db.Close()
	before, _ := os.ReadFile(dbPath)

	raw, _ := os.ReadFile(path)
	raw[len(raw)-5] ^= 0xff
	bad := filepath.Join(t.TempDir(), "bad.db.enc")
	_ = os.WriteFile(bad, raw, 0o600)

	if _, err := Restore(dbPath, bad, s.Secret, now); err == nil {
		t.Fatal("испорченный бэкап восстановился")
	}
	if _, err := Restore(dbPath, path, settings.NewKey(), now); err == nil {
		t.Fatal("с чужим ключом восстановилось")
	}
	after, _ := os.ReadFile(dbPath)
	if !bytes.Equal(before, after) {
		t.Fatal("текущая база изменилась при отказе")
	}
	for _, e := range list(t, filepath.Dir(dbPath)) {
		if strings.Contains(e, "before-restore") || strings.Contains(e, "restore-tmp") {
			t.Errorf("остался след отказавшего восстановления: %s", e)
		}
	}
}

func dbFile(t *testing.T, db *store.DB) string {
	t.Helper()
	var path string
	if err := db.R.QueryRow(`SELECT file FROM pragma_database_list WHERE name='main'`).Scan(&path); err != nil {
		t.Fatal(err)
	}
	return path
}

// Оборванный бэкап оставляет во временной папке открытую копию базы: следующий запуск её удаляет.
func TestStaleTempRemoved(t *testing.T) {
	now := t0
	s, _ := newService(t, &now)
	_ = os.MkdirAll(s.Dir, 0o700)
	old := now.Add(-time.Hour)
	stale := filepath.Join(s.Dir, ".tmp-123")
	_ = os.MkdirAll(stale, 0o700)
	_ = os.WriteFile(filepath.Join(stale, "copy.db"), []byte(marker), 0o600)
	part := filepath.Join(s.Dir, "worker-20261031-030000.db.enc.part")
	_ = os.WriteFile(part, []byte("половина"), 0o600)
	fresh := filepath.Join(s.Dir, ".tmp-live")
	_ = os.MkdirAll(fresh, 0o700) // идёт параллельный бэкап: не трогать
	for _, p := range []string{stale, part} {
		if err := os.Chtimes(p, old, old); err != nil {
			t.Fatal(err)
		}
	}
	_ = os.Chtimes(fresh, now, now)
	keep := filepath.Join(s.Dir, "заметка.txt")
	_ = os.WriteFile(keep, []byte("x"), 0o600)
	_ = os.Chtimes(keep, old, old)

	if _, err := s.Create(t.Context()); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{stale, part} {
		if _, err := os.Stat(p); err == nil {
			t.Errorf("не удалён %s", filepath.Base(p))
		}
	}
	for _, p := range []string{fresh, keep} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("удалено лишнее %s", filepath.Base(p))
		}
	}
}

// RunDue чистит остатки и тогда, когда новый бэкап ещё не нужен.
func TestRunDueSweepsWhenNotDue(t *testing.T) {
	now := t0
	s, _ := newService(t, &now)
	if err := s.RunDue(t.Context()); err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(s.Dir, ".tmp-999")
	_ = os.MkdirAll(stale, 0o700)
	_ = os.Chtimes(stale, now.Add(-time.Hour), now.Add(-time.Hour))
	now = now.Add(time.Hour)
	if err := s.RunDue(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stale); err == nil {
		t.Error("остаток не удалён")
	}
}
