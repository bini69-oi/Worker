package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/bini69-oi/worker/core"
	"github.com/bini69-oi/worker/core/coretest"
)

// coreOn собирает ядро на настоящей базе в файле, как это делает процесс при запуске.
func coreOn(t *testing.T, path string, clock *coretest.Clock, s *coretest.Sender) (*core.Core, *DB) {
	t.Helper()
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	c := core.New(core.Deps{Store: NewCoreStore(db), Clock: clock, Config: coretest.NewConfig(t), Sender: s, OwnerID: 1})
	return c, db
}

// Перезапуск посередине на настоящей SQLite: процесс остановился после 500 из 1000, а на 300-м сообщении
// ещё и не успел записать курсор. Рассылка продолжилась с курсора, никто не получил больше одного сообщения,
// кроме одного абонента на границе.
func TestBroadcastSurvivesRestart(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "restart.db")
	clock := coretest.NewClock(tNow)
	sender := &coretest.Sender{}
	c, db := coreOn(t, path, clock, sender)
	for i := range 1000 {
		if _, err := c.Register(ctx, int64(10000+i), "u", "ru"); err != nil {
			t.Fatal(err)
		}
	}
	b, err := c.CreateBroadcast(ctx, 1, core.Broadcast{Segment: core.SegAll, Text: "Привет"})
	if err != nil {
		t.Fatal(err)
	}
	if b, err = c.StartBroadcast(ctx, 1, b.ID); err != nil || b.Total != 1000 {
		t.Fatalf("старт: %+v %v", b, err)
	}

	// Процесс падает на 300-м сообщении: оно ушло, а записать его не вышло.
	dying, die := context.WithCancel(ctx)
	sender.OnSend = func(n int) {
		if n == 300 {
			die()
		}
	}
	for i := 0; i < 20 && dying.Err() == nil; i++ {
		_, _ = c.RunBroadcast(dying)
		clock.Add(time.Second)
	}
	if dying.Err() == nil {
		t.Fatal("процесс не упал")
	}
	sender.OnSend = nil
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	// Перезапуск: новая база на том же файле, новое ядро, часов не помним.
	c, db = coreOn(t, path, clock, sender)
	defer db.Close()
	for i := 0; i < 100; i++ {
		got, err := c.BroadcastByID(ctx, 1, b.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.Status != core.BcRunning {
			break
		}
		if _, err := c.RunBroadcast(ctx); err != nil {
			t.Fatal(err)
		}
		clock.Add(time.Second)
	}
	got, err := c.BroadcastByID(ctx, 1, b.ID)
	if err != nil || got.Status != core.BcDone {
		t.Fatalf("после перезапуска: %+v %v", got, err)
	}
	seen := map[int64]int{}
	for _, m := range sender.Messages() {
		seen[m.TgID]++
	}
	twice := 0
	for tg, n := range seen {
		if n > 2 || n < 1 {
			t.Fatalf("абонент %d получил %d сообщений", tg, n)
		}
		if n == 2 {
			twice++
		}
	}
	if len(seen) != 1000 || twice > 1 {
		t.Errorf("получили %d абонентов из 1000, дважды %d", len(seen), twice)
	}
}
