package store

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
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
	if _, _, err := c.SessionUser(ctx, tok2); err != core.ErrBanned {
		t.Fatalf("забаненный с живым токеном: %v", err)
	}
	if err := c.Unban(ctx, 1, u.ID, ""); err != nil {
		t.Fatal(err)
	}
	if _, _, err := c.SessionUser(ctx, tok2); err != core.ErrSessionGone {
		t.Fatalf("бан не закрыл сессию: %v", err)
	}
}

// Просроченное чистит фоновая задача, а не открытие сессии; запросы входа живут сутки, а не 30 дней.
func TestCleanCabinetOnSQLite(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "w.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	clock := coretest.NewClock(tNow)
	c := core.New(core.Deps{Store: NewCoreStore(db), Clock: clock, Config: coretest.NewConfig(t), OwnerID: 1})
	ctx := context.Background()
	u, _ := c.Register(ctx, 1001, "ivan", "ru")
	count := func(table string) int {
		var n int
		if err := db.W.QueryRow("SELECT count(*) FROM " + table).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if _, _, err := c.OpenSession(ctx, u.ID, core.SessionTg, core.LoginInfo{}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.WebLoginStart(ctx, core.LoginInfo{IP: "1.1.1.1"}); err != nil {
		t.Fatal(err)
	}
	clock.Add(12 * time.Hour)
	_, _, _ = c.OpenSession(ctx, u.ID, core.SessionTg, core.LoginInfo{})
	if count("user_sessions") != 2 {
		t.Fatal("открытие сессии не должно ничего стирать")
	}
	clock.Add(38 * time.Hour) // первая сессия истекла больше суток назад, вторая ещё нет, запрос входа тоже
	if _, err := c.CleanCabinet(ctx); err != nil {
		t.Fatal(err)
	}
	if n := count("user_logins"); n != 0 {
		t.Fatalf("запросов входа %d, ждали 0 через сутки", n)
	}
	if n := count("user_sessions"); n != 1 {
		t.Fatalf("сессий %d, ждали 1 (живая)", n)
	}
	// Запрос по expires_at идёт по индексу.
	var plan string
	rows, err := db.W.Query("EXPLAIN QUERY PLAN DELETE FROM user_sessions WHERE expires_at < 1")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var a, b, c2 int
		var d string
		_ = rows.Scan(&a, &b, &c2, &d)
		plan += d
	}
	if !strings.Contains(plan, "user_sessions_expires") {
		t.Fatalf("чистка без индекса: %s", plan)
	}
}
