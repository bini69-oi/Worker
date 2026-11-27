package core_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/bini69-oi/worker/core"
	"github.com/bini69-oi/worker/core/coretest"
)

// bcEnv это ядро с фейковым Telegram для рассылок и абонентами в разных состояниях.
type bcEnv struct {
	*env
	c      *core.Core
	sender *coretest.Sender
}

func newBcEnv(t *testing.T) *bcEnv {
	t.Helper()
	e := newEnv(t)
	s := &coretest.Sender{}
	c := core.New(core.Deps{Store: e.st, Clock: e.clock, Config: e.cfg, Panel: e.panel, Notifier: e.notes, OwnerID: owner, Sender: s})
	return &bcEnv{env: e, c: c, sender: s}
}

func (e *bcEnv) draft(t *testing.T, segment string) core.Broadcast {
	t.Helper()
	b, err := e.c.CreateBroadcast(context.Background(), owner, core.Broadcast{Segment: segment, Text: "<b>Привет</b>"})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestCreateBroadcastChecks(t *testing.T) {
	ctx := context.Background()
	e := newBcEnv(t)
	good := core.Broadcast{Segment: core.SegAll, Text: "Привет", ButtonText: "Открыть", ButtonURL: "https://example.com"}
	b, err := e.c.CreateBroadcast(ctx, owner, good)
	if err != nil || b.ID == 0 || b.Status != core.BcDraft {
		t.Fatalf("%+v %v", b, err)
	}
	cases := []struct {
		name string
		mod  func(*core.Broadcast)
		want error
	}{
		{"чужой тег", func(b *core.Broadcast) { b.Text = "<script>x</script>" }, core.ErrBadMarkup},
		{"javascript в тексте", func(b *core.Broadcast) { b.Text = `<a href="javascript:alert(1)">x</a>` }, core.ErrBadMarkup},
		{"длинный текст", func(b *core.Broadcast) { b.Text = strings.Repeat("я", 4097) }, core.ErrTooLong},
		{"неизвестный сегмент", func(b *core.Broadcast) { b.Segment = "чужие" }, core.ErrInvalid},
		{"кнопка без ссылки", func(b *core.Broadcast) { b.ButtonURL = "" }, core.ErrInvalid},
		{"ссылка без кнопки", func(b *core.Broadcast) { b.ButtonText = "" }, core.ErrInvalid},
		{"ссылка кнопки javascript", func(b *core.Broadcast) { b.ButtonURL = "javascript:alert(1)" }, core.ErrBadMarkup},
		{"ссылка кнопки http", func(b *core.Broadcast) { b.ButtonURL = "http://example.com" }, core.ErrBadMarkup},
		{"длинная подпись кнопки", func(b *core.Broadcast) { b.ButtonText = strings.Repeat("я", 65) }, core.ErrInvalid},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			bad := good
			c.mod(&bad)
			if _, err := e.c.CreateBroadcast(ctx, owner, bad); !errors.Is(err, c.want) {
				t.Fatalf("ждали %v, получили %v", c.want, err)
			}
		})
	}
	if page, _ := e.c.Broadcasts(ctx, owner, 0); len(page.Items) != 1 {
		t.Errorf("отклонённые рассылки сохранились: %d", len(page.Items))
	}
	helperWith(t, e.env, 50, core.PermUsers)
	if _, err := e.c.CreateBroadcast(ctx, 50, good); !errors.Is(err, core.ErrForbidden) {
		t.Errorf("помощник без права: %v", err)
	}
}

// Счётчик сегмента не считает забаненных и недоступных.
func TestSegmentCount(t *testing.T) {
	ctx := context.Background()
	e := newBcEnv(t)
	sub := e.bought(t) // ivan с платной подпиской
	_ = sub
	mk := func(tg int64) core.User {
		u, err := e.c.Register(ctx, tg, "u", "ru")
		if err != nil {
			t.Fatal(err)
		}
		return u
	}
	banned, gone, plain := mk(2001), mk(2002), mk(2003)
	_ = plain
	if err := e.c.Ban(ctx, owner, banned.ID, "спам"); err != nil {
		t.Fatal(err)
	}
	if err := e.c.MarkUnreachable(ctx, gone.ID); err != nil {
		t.Fatal(err)
	}
	for seg, want := range map[string]int{core.SegAll: 2, core.SegActive: 1, core.SegNone: 1, core.SegTrial: 0, core.SegExpired30: 0} {
		got, err := e.c.CountSegment(ctx, owner, seg)
		if err != nil || got != want {
			t.Errorf("сегмент %s: %d, ждали %d (%v)", seg, got, want, err)
		}
	}
	if _, err := e.c.CountSegment(ctx, owner, "чужие"); !errors.Is(err, core.ErrInvalid) {
		t.Errorf("неизвестный сегмент: %v", err)
	}
}

func TestTestBroadcastOnlyToSelf(t *testing.T) {
	ctx := context.Background()
	e := newBcEnv(t)
	for tg := int64(2001); tg < 2005; tg++ {
		if _, err := e.c.Register(ctx, tg, "u", "ru"); err != nil {
			t.Fatal(err)
		}
	}
	b := e.draft(t, core.SegAll)
	if err := e.c.TestBroadcast(ctx, owner, b.ID); err != nil {
		t.Fatal(err)
	}
	got := e.sender.Messages()
	if len(got) != 1 || got[0].TgID != owner || got[0].Text != "<b>Привет</b>" {
		t.Fatalf("ушло: %+v", got)
	}
	after, _ := e.c.BroadcastByID(ctx, owner, b.ID)
	if after.Status != core.BcDraft || after.Sent != 0 || after.Total != 0 {
		t.Errorf("тест изменил рассылку: %+v", after)
	}
	e.sender.Gone = map[int64]bool{owner: true}
	if err := e.c.TestBroadcast(ctx, owner, b.ID); !errors.Is(err, core.ErrUnreachable) {
		t.Errorf("админ не писал боту: %v", err)
	}
}

func TestOneBroadcastAtATime(t *testing.T) {
	ctx := context.Background()
	e := newBcEnv(t)
	if _, err := e.c.Register(ctx, 2001, "u", "ru"); err != nil {
		t.Fatal(err)
	}
	a, b := e.draft(t, core.SegAll), e.draft(t, core.SegAll)
	started, err := e.c.StartBroadcast(ctx, owner, a.ID)
	if err != nil || started.Status != core.BcRunning || started.Total != 2 {
		t.Fatalf("%+v %v", started, err)
	}
	if _, err := e.c.StartBroadcast(ctx, owner, b.ID); !errors.Is(err, core.ErrBroadcastBusy) {
		t.Fatalf("вторая рассылка: %v", err)
	}
	if err := e.c.CancelBroadcast(ctx, owner, a.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.c.StartBroadcast(ctx, owner, b.ID); err != nil {
		t.Errorf("после отмены первой: %v", err)
	}
	if _, err := e.c.StartBroadcast(ctx, owner, a.ID); !errors.Is(err, core.ErrInvalid) {
		t.Errorf("отменённую не запустить: %v", err)
	}
}
