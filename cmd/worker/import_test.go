package main

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bini69-oi/worker/panel/paneltest"
	"github.com/bini69-oi/worker/settings"
	"github.com/bini69-oi/worker/store"
)

func TestImportPanelCommand(t *testing.T) {
	for _, gen := range []paneltest.Generation{paneltest.V2, paneltest.V3} {
		p := paneltest.New(t, gen)
		ref := p.Add(paneltest.User{Username: "old_one", Status: "ACTIVE", ExpireAt: time.Now().AddDate(0, 1, 0),
			TelegramID: 5001, TrafficMax: 50 << 30, Strategy: "MONTH", DeviceLimit: 3})
		p.Add(paneltest.User{Username: "no_tg", Status: "ACTIVE", ExpireAt: time.Now().AddDate(0, 1, 0), Strategy: "NO_RESET"})
		env := Env{DBPath: filepath.Join(t.TempDir(), "w.db"), SecretKey: settings.NewKey(), OwnerID: 1,
			PanelURL: p.URL(), PanelToken: paneltest.Token}

		var out bytes.Buffer
		if err := command(env, []string{"import", "panel"}, nil, &out); err != nil {
			t.Fatal(err)
		}
		if s := out.String(); !strings.Contains(s, "Будет заведено подписок: 1") || !strings.Contains(s, "нет Telegram ID: 1") {
			t.Fatalf("v%d пробный отчёт: %s", gen, s)
		}
		if subs := subsIn(t, env); subs != 0 {
			t.Fatalf("v%d пробный прогон записал %d подписок", gen, subs)
		}

		out.Reset()
		if err := command(env, []string{"import", "panel", "--apply"}, nil, &out); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out.String(), "Заведено подписок: 1") {
			t.Fatalf("v%d отчёт: %s", gen, out.String())
		}
		db, err := store.Open(env.DBPath)
		if err != nil {
			t.Fatal(err)
		}
		cs := store.NewCoreStore(db)
		u, err := cs.UserByTg(context.Background(), 5001)
		if err != nil {
			t.Fatal(err)
		}
		sub, err := cs.SubByUser(context.Background(), u.ID)
		db.Close()
		if err != nil || sub.PanelUUID != ref || !sub.Imported || sub.Own.TrafficBytes != 50<<30 || sub.Own.Username != "old_one" {
			t.Fatalf("v%d подписка: %+v %v", gen, sub, err)
		}
		if p.Calls("PATCH /api/users")+p.Calls("POST /api/users") != 0 {
			t.Fatalf("v%d импорт писал в панель", gen)
		}
	}
}

func TestImportRefusesWhileBotRuns(t *testing.T) {
	env := Env{DBPath: filepath.Join(t.TempDir(), "w.db"), SecretKey: settings.NewKey(), OwnerID: 1}
	unlock, err := lockDB(env.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	if err := command(env, []string{"import", "panel", "--apply"}, nil, &bytes.Buffer{}); err != errLocked {
		t.Fatalf("импорт при работающем боте: %v", err)
	}
}

func subsIn(t *testing.T, env Env) int {
	t.Helper()
	db, err := store.Open(env.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var n int
	if err := db.R.QueryRow("SELECT count(*) FROM subscriptions").Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}
