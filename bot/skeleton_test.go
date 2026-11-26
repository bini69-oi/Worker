package bot

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bini69-oi/worker/tg"
)

func TestSameUserInOrder(t *testing.T) {
	e := newEnv(t)
	var busy, overlap atomic.Int32
	var mu sync.Mutex
	var order []string
	done := make(chan struct{}, 3)
	e.b.acts["slow"] = func(ctx context.Context, r *req, d Data) error {
		if busy.Add(1) > 1 {
			overlap.Add(1)
		}
		time.Sleep(20 * time.Millisecond)
		mu.Lock()
		order = append(order, d.Args[0])
		mu.Unlock()
		busy.Add(-1)
		done <- struct{}{}
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	in := make(chan tg.Update)
	go e.b.Run(ctx, in)
	for i, arg := range []string{"1", "2", "3"} {
		q := &tg.CallbackQuery{ID: arg, From: tg.User{ID: userTg},
			Message: &tg.Message{MessageID: 5, Chat: tg.Chat{ID: userTg, Type: "private"}}, Data: "slow:" + arg}
		in <- tg.Update{UpdateID: int64(i + 1), CallbackQuery: q}
	}
	for range 3 {
		<-done
	}
	if overlap.Load() != 0 || strings.Join(order, "") != "123" {
		t.Fatalf("нажатия одного абонента шли не по очереди: пересечений %d, порядок %v", overlap.Load(), order)
	}
}

func TestRunSkipsRepeatedUpdate(t *testing.T) {
	e := newEnv(t)
	var n atomic.Int32
	done := make(chan struct{}, 2)
	e.b.acts["hit"] = func(context.Context, *req, Data) error { n.Add(1); done <- struct{}{}; return nil }
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	in := make(chan tg.Update)
	go e.b.Run(ctx, in)
	q := &tg.CallbackQuery{ID: "1", From: tg.User{ID: userTg}, Message: &tg.Message{MessageID: 5, Chat: tg.Chat{ID: userTg, Type: "private"}}, Data: "hit"}
	in <- tg.Update{UpdateID: 7, CallbackQuery: q}
	in <- tg.Update{UpdateID: 7, CallbackQuery: q}
	<-done
	time.Sleep(30 * time.Millisecond)
	if n.Load() != 1 {
		t.Fatalf("повтор обновления обработан: %d", n.Load())
	}
}

func TestParseData(t *testing.T) {
	good := map[string]Data{
		"m":            {Act: "m", Args: []string{}},
		"buy:3:30":     {Act: "buy", Args: []string{"3", "30"}},
		"dev:del:ab-1": {Act: "dev", Args: []string{"del", "ab-1"}},
	}
	for in, want := range good {
		got, err := parseData(in)
		if err != nil || got.Act != want.Act || strings.Join(got.Args, ",") != strings.Join(want.Args, ",") {
			t.Fatalf("%q: получили %+v %v", in, got, err)
		}
	}
	for _, bad := range []string{"", ":1", "BUY!:1", "a:<script>", strings.Repeat("a", 65), "a:1:2:3:4:5", "a:" + strings.Repeat("1", 33)} {
		if _, err := parseData(bad); err == nil {
			t.Fatalf("%q разобрано", bad)
		}
	}
	if d, _ := parseData("buy:x:-5"); func() bool { _, ok := d.Int(0); _, ok2 := d.Int(1); return ok || ok2 }() {
		t.Fatal("не число принято за id")
	}
}

func FuzzParseData(f *testing.F) {
	for _, s := range []string{"buy:3:30", "m", "dev:del:ab", ":::", "\x00"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		d, err := parseData(s)
		if err != nil {
			return
		}
		if len(s) > maxData || d.Act == "" {
			t.Fatalf("принято плохое: %q", s)
		}
		if data(d.Act, toAny(d.Args)...) != s {
			t.Fatalf("сборка не повторяет разбор: %q", s)
		}
	})
}

func toAny(ss []string) []any {
	out := make([]any, len(ss))
	for i, s := range ss {
		out[i] = s
	}
	return out
}

func TestUnknownButtonShowsMenu(t *testing.T) {
	e := newEnv(t)
	e.press(t, userTg, 5, "nope:1")
	calls := e.api.take()
	if len(calls) < 2 || calls[0].method != "answer" || calls[0].text != "Кнопка устарела, вот главное меню" || calls[1].method != "edit" {
		t.Fatalf("устаревшая кнопка: %s", render(calls))
	}
}

func TestEditOrSend(t *testing.T) {
	e := newEnv(t)
	e.press(t, userTg, 5, "m")
	if c := e.api.take(); c[0].method != "edit" || c[0].msg != 5 {
		t.Fatalf("переход не правит сообщение: %s", render(c))
	}

	// Сообщение удалено: бот шлёт новое и запоминает его.
	e.api.editErr = &tg.Error{Code: 400, Description: "Bad Request: message to edit not found"}
	e.press(t, userTg, 5, "m")
	calls := e.api.take()
	var sent call
	for _, c := range calls {
		if c.method == "send" {
			sent = c
		}
	}
	if sent.msg == 0 {
		t.Fatalf("новое сообщение не отправлено: %s", render(calls))
	}
	u, err := e.c.UserByTg(context.Background(), userTg)
	if err != nil || u.ScreenMsgID != int64(sent.msg) {
		t.Fatalf("id нового экрана не запомнен: %d против %d (%v)", u.ScreenMsgID, sent.msg, err)
	}

	// Текст не изменился: Telegram ругается, бот молчит и нового не шлёт.
	e.api.editErr = &tg.Error{Code: 400, Description: "Bad Request: message is not modified"}
	e.press(t, userTg, 5, "m")
	for _, c := range e.api.take() {
		if c.method == "send" {
			t.Fatal("на «не изменилось» бот прислал новое сообщение")
		}
	}
}

func TestGoldenMenu(t *testing.T) {
	e := newEnv(t)
	e.press(t, userTg, 5, "m")
	golden(t, "skeleton_menu", e.api.take())
}
