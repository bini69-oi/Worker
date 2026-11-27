package store

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/bini69-oi/worker/core"
	"github.com/bini69-oi/worker/core/coretest"
)

// TestUserSessionOnSQLite проверяет сессии абонента на настоящей базе: срок, выход, бан и то, что токена в файле нет.
func TestUserSessionOnSQLite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "w.db")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	clock := coretest.NewClock(tNow)
	c := core.New(core.Deps{Store: NewCoreStore(db), Clock: clock, Config: coretest.NewConfig(t), OwnerID: 1})
	ctx := context.Background()
	u, err := c.Register(ctx, 1001, "ivan", "ru")
	if err != nil {
		t.Fatal(err)
	}
	tok, _, err := c.OpenSession(ctx, u.ID, core.SessionWeb, core.LoginInfo{IP: "1.2.3.4"})
	if err != nil {
		t.Fatal(err)
	}
	if got, _, err := c.SessionUser(ctx, tok); err != nil || got.ID != u.ID {
		t.Fatalf("сессия: %v", err)
	}
	for _, f := range []string{path, path + "-wal"} {
		if raw, err := os.ReadFile(f); err == nil && bytes.Contains(raw, []byte(tok)) {
			t.Fatalf("токен лежит в %s открытым", f)
		}
	}
	clock.Add(8 * 24 * time.Hour)
	if _, _, err := c.SessionUser(ctx, tok); err != core.ErrSessionGone {
		t.Fatalf("неделя без запросов: %v", err)
	}
	tok2, _, _ := c.OpenSession(ctx, u.ID, core.SessionTg, core.LoginInfo{})
	if err := c.Ban(ctx, 1, u.ID, ""); err != nil {
		t.Fatal(err)
	}
	if _, _, err := c.SessionUser(ctx, tok2); err != core.ErrSessionGone {
		t.Fatalf("бан не закрыл сессию: %v", err)
	}
}
