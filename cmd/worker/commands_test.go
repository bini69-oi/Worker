package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bini69-oi/worker/core"
	"github.com/bini69-oi/worker/settings"
	"github.com/bini69-oi/worker/store"
)

func TestRotateCommand(t *testing.T) {
	env := Env{DBPath: filepath.Join(t.TempDir(), "w.db"), SecretKey: settings.NewKey()}
	db, err := store.Open(env.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	s, err := settings.New(store.NewSettingsStore(db), env.SecretKey, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetSecret("yookassa.secret_key", "live_key_one"); err != nil {
		t.Fatal(err)
	}
	db.Close()

	var out bytes.Buffer
	if err := command(env, []string{"rotate"}, nil, &out); err != nil {
		t.Fatal(err)
	}
	saved, err := os.ReadFile(env.DBPath + ".newkey")
	if err != nil || !strings.HasPrefix(string(saved), "SECRET_KEY=") {
		t.Fatalf("новый ключ не сохранён в файл: %v", err)
	}
	if st, _ := os.Stat(env.DBPath + ".newkey"); st.Mode().Perm() != 0o600 {
		t.Fatalf("файл ключа читают другие: %v", st.Mode())
	}
	line := out.String()[strings.Index(out.String(), "SECRET_KEY=")+len("SECRET_KEY="):]
	newKey, err := base64.StdEncoding.DecodeString(strings.TrimSpace(line))
	if err != nil || len(newKey) != 32 {
		t.Fatalf("команда не напечатала новый ключ: %q", out.String())
	}

	db, _ = store.Open(env.DBPath)
	defer db.Close()
	fresh, err := settings.New(store.NewSettingsStore(db), newKey, time.Now)
	if err != nil || fresh.Secret("yookassa.secret_key") != "live_key_one" {
		t.Fatalf("новым ключом секрет не читается: %v", err)
	}
}

func TestUnknownCommand(t *testing.T) {
	if err := command(Env{}, []string{"drop"}, nil, &bytes.Buffer{}); err == nil {
		t.Fatal("неизвестная команда должна давать ошибку")
	}
}

func TestAdminPasswordCommand(t *testing.T) {
	env := Env{DBPath: filepath.Join(t.TempDir(), "w.db"), OwnerID: 1}
	var out bytes.Buffer
	if err := command(env, []string{"admin", "password"}, strings.NewReader("short\nshort\n"), &out); err == nil {
		t.Fatal("короткий пароль принят")
	}
	if err := command(env, []string{"admin", "password"}, strings.NewReader("owner-password-1\nдругой-пароль\n"), &out); err == nil {
		t.Fatal("несовпавшие пароли приняты")
	}
	if err := command(env, []string{"admin", "password"}, strings.NewReader("owner-password-1\nowner-password-1\n"), &out); err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(env.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	c := core.New(core.Deps{Store: store.NewCoreStore(db), OwnerID: 1})
	if _, err := c.LoginStart(context.Background(), 1, "owner-password-1", core.LoginInfo{IP: "1.1.1.1"}); err != nil {
		t.Fatalf("вход с заданным паролем: %v", err)
	}
}

// Повторный rotate, когда новый ключ ещё не перенесён в .env, не должен терять ключ прошлого запуска.
func TestRotateTwiceKeepsKey(t *testing.T) {
	env := Env{DBPath: filepath.Join(t.TempDir(), "w.db"), SecretKey: settings.NewKey()}
	db, err := store.Open(env.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	s, err := settings.New(store.NewSettingsStore(db), env.SecretKey, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetSecret("yookassa.secret_key", "live_key_one"); err != nil {
		t.Fatal(err)
	}
	db.Close()
	if err := command(env, []string{"rotate"}, nil, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	first, _ := os.ReadFile(keyFile(env.DBPath))
	if err := command(env, []string{"rotate"}, nil, &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "не закончена") {
		t.Fatalf("второй rotate должен отказать: %v", err)
	}
	if again, _ := os.ReadFile(keyFile(env.DBPath)); !bytes.Equal(first, again) {
		t.Fatal("второй rotate затёр ключ первого")
	}

	// Старт со старым ключом отказывает и подсказывает, что делать.
	if _, err := checkKeyFile(env); err == nil {
		t.Fatal("старт со старым ключом при незаконченной смене")
	}
	// Ключ перенесён в .env: файл удаляется при старте.
	newKey, _ := base64.StdEncoding.DecodeString(strings.TrimSpace(strings.TrimPrefix(string(first), "SECRET_KEY=")))
	env.SecretKey = newKey
	if msg, err := checkKeyFile(env); err != nil || msg == "" {
		t.Fatalf("файл с перенесённым ключом: %q %v", msg, err)
	}
	if _, err := os.Stat(keyFile(env.DBPath)); !os.IsNotExist(err) {
		t.Fatal("файл ключа остался рядом с базой")
	}
}

func TestRotateRefusesWhileBotRuns(t *testing.T) {
	env := Env{DBPath: filepath.Join(t.TempDir(), "w.db"), SecretKey: settings.NewKey()}
	unlock, err := lockDB(env.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	if err := command(env, []string{"rotate"}, nil, &bytes.Buffer{}); err != errLocked {
		t.Fatalf("rotate при работающем боте: %v", err)
	}
	if _, err := os.Stat(keyFile(env.DBPath)); !os.IsNotExist(err) {
		t.Fatal("rotate при работающем боте оставил файл ключа")
	}
}

// worker admin allow-all: владелец, запершийся списком адресов, сбрасывает его на сервере, запись идёт в журнал.
func TestAdminAllowAllCommand(t *testing.T) {
	env := Env{DBPath: filepath.Join(t.TempDir(), "w.db"), SecretKey: settings.NewKey(), OwnerID: 1}
	db, err := store.Open(env.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	s, err := settings.New(store.NewSettingsStore(db), env.SecretKey, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Set("admin.allow_ips", "203.0.113.5"); err != nil {
		t.Fatal(err)
	}
	db.Close()

	var out bytes.Buffer
	if err := command(env, []string{"admin", "allow-all"}, nil, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "сброшен") {
		t.Errorf("вывод: %q", out.String())
	}
	db, _ = store.Open(env.DBPath)
	defer db.Close()
	fresh, err := settings.New(store.NewSettingsStore(db), env.SecretKey, time.Now)
	if err != nil || fresh.String("admin.allow_ips") != "" {
		t.Fatalf("список не сброшен: %q %v", fresh.String("admin.allow_ips"), err)
	}
	rows, err := store.NewCoreStore(db).AuditLog(context.Background(), core.AuditFilter{Action: "admin.allow_all"}, 5)
	if err != nil || len(rows) != 1 || rows[0].AdminTg != 1 || rows[0].Details["old"] != "203.0.113.5" {
		t.Errorf("журнал: %+v %v", rows, err)
	}
}
