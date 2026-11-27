package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bini69-oi/worker/core"
	"github.com/bini69-oi/worker/panel"
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
		if subs := subsIn(t, env); subs != 0 { // база уже есть, как у работавшего бота
			t.Fatal(subs)
		}

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

func TestImportNeedsExistingDB(t *testing.T) {
	env := Env{DBPath: filepath.Join(t.TempDir(), "typo.db"), SecretKey: settings.NewKey(), OwnerID: 1}
	if err := command(env, []string{"import", "panel"}, nil, &bytes.Buffer{}); err == nil {
		t.Fatal("импорт в несуществующую базу прошёл")
	}
	if _, err := os.Stat(env.DBPath); !os.IsNotExist(err) {
		t.Fatal("импорт создал базу по опечатке")
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

func mustUser(t *testing.T, db *store.DB, tg int64) int64 {
	t.Helper()
	u, err := store.NewCoreStore(db).UserByTg(context.Background(), tg)
	if err != nil {
		t.Fatal(err)
	}
	return u.ID
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

// Пользователь LIMITED (кончился трафик) после переноса и сверки не включается заново, заметка владельца
// в описании панели остаётся.
func TestImportedLimitedUntouchedBySync(t *testing.T) {
	p := paneltest.New(t, paneltest.V2)
	p.Add(paneltest.User{Username: "lim", Status: "LIMITED", ExpireAt: time.Now().AddDate(0, 1, 0),
		TelegramID: 5001, TrafficMax: 50 << 30, Strategy: "MONTH", DeviceLimit: 3, Description: "заметка владельца"})
	env := Env{DBPath: filepath.Join(t.TempDir(), "w.db"), SecretKey: settings.NewKey(), OwnerID: 1,
		PanelURL: p.URL(), PanelToken: paneltest.Token}
	subsIn(t, env)
	if err := command(env, []string{"import", "panel", "--apply"}, nil, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(env.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	cfg, err := settings.New(store.NewSettingsStore(db), env.SecretKey, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	c := core.New(core.Deps{Store: store.NewCoreStore(db), Config: cfg, OwnerID: 1,
		Panel: panel.CoreAdapter{Client: panel.New(env.PanelURL, env.PanelToken, nil)}})
	if n, err := c.Reconcile(context.Background()); err != nil || n != 0 {
		t.Fatalf("сверка нашла расхождение у LIMITED: %d %v", n, err)
	}
	u := p.Users()[0]
	if u.Status != "LIMITED" || u.Description != "заметка владельца" {
		t.Fatalf("панель после сверки: %s %q", u.Status, u.Description)
	}
	// Синхронизация всё равно случится (выдача дней, истечение): описание и имя не трогаются.
	if _, err := c.GrantDays(context.Background(), 1, mustUser(t, db, 5001), 3); err != nil {
		t.Fatal(err)
	}
	if _, err := c.ProcessDue(context.Background()); err != nil {
		t.Fatal(err)
	}
	if u := p.Users()[0]; u.Username != "lim" || u.Description != "заметка владельца" || u.TrafficMax != 50<<30 {
		t.Fatalf("панель после синхронизации: %+v", u)
	}
}
