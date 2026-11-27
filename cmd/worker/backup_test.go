package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bini69-oi/worker/settings"
	"github.com/bini69-oi/worker/store"
)

func backupEnv(t *testing.T) Env {
	t.Helper()
	env := Env{DBPath: filepath.Join(t.TempDir(), "w.db"), SecretKey: settings.NewKey()}
	db, err := store.Open(env.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	return env
}

func TestBackupCommand(t *testing.T) {
	env := backupEnv(t)
	var out bytes.Buffer
	if err := command(env, []string{"backup"}, nil, &out); err != nil {
		t.Fatal(err)
	}
	files, _ := filepath.Glob(filepath.Join(filepath.Dir(env.DBPath), "backups", "worker-*.db.enc"))
	if len(files) != 1 {
		t.Fatalf("бэкап не создан: %v", files)
	}
	if !strings.Contains(out.String(), files[0]) {
		t.Errorf("команда не назвала файл: %q", out.String())
	}
	if strings.Contains(out.String(), "SECRET_KEY") {
		t.Error("вывод упоминает ключ")
	}
}

func TestBackupWorksWhileBotRuns(t *testing.T) {
	env := backupEnv(t)
	unlock, err := lockDB(env.DBPath) // так выглядит работающий бот
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	if err := command(env, []string{"backup"}, nil, &bytes.Buffer{}); err != nil {
		t.Fatalf("бэкап без остановки бота не получился: %v", err)
	}
}

func TestBackupUnknownFlag(t *testing.T) {
	if err := command(backupEnv(t), []string{"backup", "--нет"}, nil, &bytes.Buffer{}); err == nil {
		t.Fatal("лишний аргумент должен давать ошибку")
	}
}

func makeBackup(t *testing.T, env Env) string {
	t.Helper()
	var out bytes.Buffer
	if err := command(env, []string{"backup"}, nil, &out); err != nil {
		t.Fatal(err)
	}
	files, _ := filepath.Glob(filepath.Join(filepath.Dir(env.DBPath), "backups", "worker-*.db.enc"))
	if len(files) != 1 {
		t.Fatalf("бэкап не создан: %v", files)
	}
	return files[0]
}

func TestBackupCheckCommand(t *testing.T) {
	env := backupEnv(t)
	file := makeBackup(t, env)
	var out bytes.Buffer
	if err := command(env, []string{"backup", "--check", file}, nil, &out); err != nil {
		t.Fatalf("целый бэкап не прошёл проверку: %v", err)
	}
	if !strings.Contains(out.String(), "в порядке") {
		t.Errorf("вывод: %q", out.String())
	}
	other := env
	other.SecretKey = settings.NewKey()
	if err := command(other, []string{"backup", "--check", file}, nil, &bytes.Buffer{}); err == nil {
		t.Error("проверка с чужим ключом прошла")
	}
	if err := command(env, []string{"backup", "--check"}, nil, &bytes.Buffer{}); err == nil {
		t.Error("без файла нужна ошибка")
	}
}

func TestRestoreCommand(t *testing.T) {
	env := backupEnv(t)
	file := makeBackup(t, env)
	var out bytes.Buffer
	if err := command(env, []string{"restore", file}, nil, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "before-restore") {
		t.Errorf("вывод не говорит, куда отложена прежняя база: %q", out.String())
	}
	if err := store.CheckFile(env.DBPath); err != nil {
		t.Fatal(err)
	}
}

func TestRestoreRefusesWhileBotRuns(t *testing.T) {
	env := backupEnv(t)
	file := makeBackup(t, env)
	before, _ := os.ReadFile(env.DBPath)
	unlock, err := lockDB(env.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	err = command(env, []string{"restore", file}, nil, &bytes.Buffer{})
	if !errors.Is(err, errLocked) {
		t.Fatalf("при работающем боте ждали errLocked, получили %v", err)
	}
	after, _ := os.ReadFile(env.DBPath)
	if !bytes.Equal(before, after) {
		t.Fatal("база изменилась при отказе")
	}
}

func TestRestoreCorruptedFile(t *testing.T) {
	env := backupEnv(t)
	file := makeBackup(t, env)
	raw, _ := os.ReadFile(file)
	raw[len(raw)/2] ^= 0xff
	if err := os.WriteFile(file, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(env.DBPath)
	if err := command(env, []string{"restore", file}, nil, &bytes.Buffer{}); err == nil {
		t.Fatal("испорченный бэкап восстановился")
	}
	after, _ := os.ReadFile(env.DBPath)
	if !bytes.Equal(before, after) {
		t.Fatal("текущая база тронута")
	}
}

func TestRestoreNeedsFile(t *testing.T) {
	if err := command(backupEnv(t), []string{"restore"}, nil, &bytes.Buffer{}); err == nil {
		t.Fatal("без файла нужна ошибка")
	}
}
