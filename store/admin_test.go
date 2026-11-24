package store

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/bini69-oi/worker/core"
	"github.com/bini69-oi/worker/core/coretest"
)

// TestLoginOnSQLite проходит вход на настоящей базе и проверяет, что в файлах базы нет самого токена.
func TestLoginOnSQLite(t *testing.T) {
	dir := t.TempDir()
	db, err := Open(filepath.Join(dir, "w.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s := NewCoreStore(db)
	ctx := context.Background()
	notes := &coretest.Notifier{}
	c := core.New(core.Deps{Store: s, Clock: coretest.NewClock(tNow), Config: coretest.NewConfig(t), Notifier: notes, OwnerID: 1})
	if err := c.SetOwnerPassword(ctx, "owner-password-1"); err != nil {
		t.Fatal(err)
	}
	id, nonce, err := c.LoginStart(ctx, 1, "owner-password-1", core.LoginInfo{IP: "1.2.3.4", Agent: "Firefox"})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.AnswerConfirm(ctx, id, 1, true); err != nil {
		t.Fatal(err)
	}
	if err := c.AnswerConfirm(ctx, id, 1, true); err != core.ErrConfirmGone {
		t.Fatalf("второе нажатие на SQLite: %v", err)
	}
	token, sess, err := c.LoginFinish(ctx, id, nonce, core.LoginInfo{IP: "1.2.3.4", Agent: "Firefox"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := c.SessionAdmin(ctx, token); err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{"w.db", "w.db-wal"} {
		raw, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		if bytes.Contains(raw, []byte(token)) || bytes.Contains(raw, []byte(nonce)) {
			t.Fatalf("в файле %s лежит токен сессии или ключ браузера", name)
		}
	}

	if err := c.CloseSession(ctx, 1, sess.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := c.SessionAdmin(ctx, token); err != core.ErrSessionGone {
		t.Fatalf("закрытая сессия на SQLite работает: %v", err)
	}
	var audits int
	if err := db.R.QueryRow("SELECT COUNT(*) FROM audit_log WHERE action IN ('login', 'session.close')").Scan(&audits); err != nil || audits != 2 {
		t.Fatalf("вход и закрытие сессии не в журнале: %d (%v)", audits, err)
	}
}

func TestAdminChatOnSQLite(t *testing.T) {
	s := NewCoreStore(openTest(t))
	ctx := context.Background()
	c := core.New(core.Deps{Store: s, Clock: coretest.NewClock(tNow), Config: coretest.NewConfig(t), OwnerID: 1})
	code, err := c.NewChatCode(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.BindChat(ctx, -100, 1, code); err != nil {
		t.Fatal(err)
	}
	if err := c.SetChatTopics(ctx, map[string]int64{core.TopicPayments: 7}); err != nil {
		t.Fatal(err)
	}
	ch, err := c.AdminChat(ctx)
	if err != nil || ch.ChatID != -100 || ch.Topics[core.TopicPayments] != 7 || len(ch.CodeHash) != 0 {
		t.Fatalf("группа на SQLite: %+v %v", ch, err)
	}
}
