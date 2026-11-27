package main

import (
	"bytes"
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
