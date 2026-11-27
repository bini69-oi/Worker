package core_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/bini69-oi/worker/core"
)

// start просит запуск и отвечает «Да» владельца: без него рассылка не идёт.
func (e *bcEnv) start(actor, id int64) (core.Broadcast, error) {
	ctx := context.Background()
	cid, err := e.c.StartBroadcast(ctx, actor, id)
	if err != nil {
		return core.Broadcast{}, err
	}
	if err := e.c.AnswerConfirm(ctx, cid, owner, true); err != nil {
		return core.Broadcast{}, err
	}
	return e.c.BroadcastByID(ctx, owner, id)
}

// bigEnv заводит n абонентов (с Telegram ID 10000+i), запускает рассылку всем и отдаёт её.
func (e *bcEnv) running(t *testing.T, n int) core.Broadcast {
	t.Helper()
	ctx := context.Background()
	for i := range n {
		if _, err := e.c.Register(ctx, int64(10000+i), "u", "ru"); err != nil {
			t.Fatal(err)
		}
	}
	b, err := e.start(owner, e.draft(t, core.SegAll).ID)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// tick это одна секунда работы задачи: пачка, потом часы идут вперёд. Отдаёт число отправленных.
func (e *bcEnv) tick(t *testing.T) int {
	t.Helper()
	n, err := e.c.RunBroadcast(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	e.clock.Add(time.Second)
	return n
}

func (e *bcEnv) state(t *testing.T, id int64) core.Broadcast {
	t.Helper()
	b, err := e.c.BroadcastByID(context.Background(), owner, id)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// 1000 абонентов: не быстрее 25 в секунду, значит не меньше 40 секунд, счётчики в сумме дают 1000.
func TestBroadcastThousand(t *testing.T) {
	e := newBcEnv(t)
	e.sender.Gone = map[int64]bool{}
	e.sender.Fail = map[int64]error{}
	for i := 0; i < 1000; i += 20 {
		e.sender.Gone[int64(10000+i)] = true // 50 заблокировали бота
	}
	for i := 5; i < 1000; i += 33 {
		e.sender.Fail[int64(10000+i)] = errors.New("chat not found") // прочие сбои
	}
	b := e.running(t, 1000)
	if b.Total != 1001 { // 1000 новых и ivan из newEnv
		t.Fatalf("получателей на старте %d", b.Total)
	}
	start := e.clock.Now()
	seconds := 0
	for ; e.state(t, b.ID).Status == core.BcRunning && seconds < 200; seconds++ {
		if n := e.tick(t); n > 25 {
			t.Fatalf("за секунду ушло %d сообщений", n)
		}
	}
	got := e.state(t, b.ID)
	if got.Status != core.BcDone {
		t.Fatalf("не закончена: %+v", got)
	}
	if elapsed := e.clock.Now().Sub(start); elapsed < 40*time.Second {
		t.Errorf("рассылка шла %v, меньше 40 секунд", elapsed)
	}
	wantFailed := 0
	for tg := range e.sender.Fail {
		if !e.sender.Gone[tg] {
			wantFailed++
		}
	}
	if got.Sent+got.Failed+got.Blocked != b.Total || got.Blocked != 50 || got.Failed != wantFailed {
		t.Errorf("счётчики: %+v при %d получателях", got, b.Total)
	}
	// Заблокировавшие бота отмечены недоступными и больше не получают рассылок.
	if n, _ := e.c.CountSegment(context.Background(), owner, core.SegAll); n != b.Total-50 {
		t.Errorf("после рассылки в сегменте %d", n)
	}
	seen := map[int64]int{}
	for _, m := range e.sender.Messages() {
		seen[m.TgID]++
	}
	for tg, n := range seen {
		if n != 1 {
			t.Fatalf("абонент %d получил %d сообщений", tg, n)
		}
	}
}

func TestBroadcastRateIsHeldByCore(t *testing.T) {
	e := newBcEnv(t)
	b := e.running(t, 100)
	ctx := context.Background()
	if n, _ := e.c.RunBroadcast(ctx); n != 25 {
		t.Fatalf("первая пачка: %d", n)
	}
	// Задачу зовут чаще раза в секунду: ядро не отправит ничего, пока не пройдёт секунда.
	for range 5 {
		if n, _ := e.c.RunBroadcast(ctx); n != 0 {
			t.Fatalf("пачка раньше срока: %d", n)
		}
	}
	e.clock.Add(time.Second)
	if n, _ := e.c.RunBroadcast(ctx); n != 25 {
		t.Errorf("вторая пачка: %d", n)
	}
	if e.state(t, b.ID).Sent != 50 {
		t.Errorf("счётчик: %+v", e.state(t, b.ID))
	}
}

func TestBroadcastRetryAfter(t *testing.T) {
	e := newBcEnv(t)
	b := e.running(t, 60)
	e.tick(t) // 25 ушло
	e.sender.Wait = 7
	if n := e.tick(t); n != 0 {
		t.Fatalf("429 не остановил пачку: %d", n)
	}
	cursor := e.state(t, b.ID).CursorUserID
	for range 6 { // 6 секунд из 7 ждём: ничего не уходит
		if n := e.tick(t); n != 0 {
			t.Fatalf("отправка во время ожидания retry_after: %d", n)
		}
	}
	if e.state(t, b.ID).CursorUserID != cursor {
		t.Errorf("курсор сдвинулся при ожидании")
	}
	for e.state(t, b.ID).Status == core.BcRunning {
		e.tick(t)
	}
	got := e.state(t, b.ID)
	if got.Sent != got.Total || got.Failed != 0 {
		t.Errorf("после 429 кто-то потерян: %+v", got)
	}
	seen := map[int64]int{}
	for _, m := range e.sender.Messages() {
		seen[m.TgID]++
		if seen[m.TgID] > 1 {
			t.Fatalf("абонент %d получил повтор после 429", m.TgID)
		}
	}
}

func TestBroadcastCancel(t *testing.T) {
	e := newBcEnv(t)
	b := e.running(t, 1000)
	e.sender.OnSend = func(n int) {
		if n == 300 {
			if err := e.c.CancelBroadcast(context.Background(), owner, b.ID); err != nil {
				t.Error(err)
			}
		}
	}
	for range 60 {
		e.tick(t)
	}
	got := e.state(t, b.ID)
	if got.Status != core.BcCancelled || got.Sent != 300 || len(e.sender.Messages()) != 300 {
		t.Fatalf("после отмены: %+v, ушло %d", got, len(e.sender.Messages()))
	}
}

// Перезапуск процесса на 500 из 1000: рассылка продолжается с курсора, повторов нет.
func TestBroadcastRestartInTheMiddle(t *testing.T) {
	e := newBcEnv(t)
	b := e.running(t, 999) // с ivan ровно 1000
	if b.Total != 1000 {
		t.Fatalf("получателей %d", b.Total)
	}
	for e.state(t, b.ID).Sent < 500 {
		e.tick(t)
	}
	if got := e.state(t, b.ID); got.Sent != 500 || got.Status != core.BcRunning {
		t.Fatalf("до перезапуска: %+v", got)
	}
	// Новое ядро на той же базе: процесс перезапустился, память о паузах пропала.
	e.c = core.New(core.Deps{Store: e.st, Clock: e.clock, Config: e.cfg, Panel: e.panel, Notifier: e.notes, OwnerID: owner, Sender: e.sender})
	for i := 0; e.state(t, b.ID).Status == core.BcRunning && i < 100; i++ {
		e.tick(t)
	}
	if got := e.state(t, b.ID); got.Status != core.BcDone || got.Sent != 1000 {
		t.Fatalf("после перезапуска: %+v", got)
	}
	seen := map[int64]int{}
	for _, m := range e.sender.Messages() {
		seen[m.TgID]++
	}
	if len(seen) != 1000 {
		t.Errorf("получили %d абонентов из 1000", len(seen))
	}
	for tg, n := range seen {
		if n > 1 {
			t.Errorf("абонент %d получил %d сообщений", tg, n)
		}
	}
}

func TestBroadcastDoneNotice(t *testing.T) {
	e := newBcEnv(t)
	b := e.running(t, 3)
	for e.state(t, b.ID).Status == core.BcRunning {
		e.tick(t)
	}
	found := false
	for _, n := range adminTasks(t, e.env) {
		found = found || (n.Key == "admin.broadcast_done" && n.Vars["sent"] == "4")
	}
	if !found {
		t.Errorf("итог рассылки не ушёл в тему")
	}
	// Законченная рассылка больше не шлёт.
	if n := e.tick(t); n != 0 {
		t.Errorf("после конца ушло %d", n)
	}
}
