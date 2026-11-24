package main

import (
	"bytes"
	"context"
	"encoding/base64"
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
	if err := s.Set("yookassa.secret_key", "live_key_one"); err != nil {
		t.Fatal(err)
	}
	db.Close()

	var out bytes.Buffer
	if err := command(env, []string{"rotate"}, nil, &out); err != nil {
		t.Fatal(err)
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
	if _, _, err := c.LoginStart(context.Background(), 1, "owner-password-1", core.LoginInfo{IP: "1.1.1.1"}); err != nil {
		t.Fatalf("вход с заданным паролем: %v", err)
	}
}
