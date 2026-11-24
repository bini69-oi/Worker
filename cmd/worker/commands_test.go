package main

import (
	"bytes"
	"encoding/base64"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
