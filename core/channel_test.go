package core_test

import (
	"context"
	"errors"
	"testing"

	"github.com/bini69-oi/worker/core"
)

type fakeChannels struct {
	member bool
	err    error
	asked  []string
}

func (f *fakeChannels) IsMember(_ context.Context, channel string, tgID int64) (bool, error) {
	f.asked = append(f.asked, channel)
	return f.member, f.err
}

func channelEnv(t *testing.T, ch *fakeChannels) *env {
	t.Helper()
	e := newEnv(t)
	for k, v := range map[string]string{"trial.enabled": "true", "trial.days": "3", "trial.channel": "@news"} {
		if err := e.cfg.Set(k, v); err != nil {
			t.Fatal(err)
		}
	}
	e.c = core.New(core.Deps{Store: e.st, Clock: e.clock, Config: e.cfg, Panel: e.panel, Notifier: e.notes,
		OwnerID: owner, Channels: ch})
	return e
}

func TestTrialNeedsChannel(t *testing.T) {
	ctx := context.Background()
	ch := &fakeChannels{}
	e := channelEnv(t, ch)
	if _, err := e.c.Trial(ctx, e.user.ID); !errors.Is(err, core.ErrNotSubscribed) {
		t.Fatalf("не подписан: ждали ErrNotSubscribed, получили %v", err)
	}
	if len(ch.asked) != 1 || ch.asked[0] != "@news" {
		t.Fatalf("спросили не тот канал: %v", ch.asked)
	}

	ch.member = true
	if _, err := e.c.Trial(ctx, e.user.ID); err != nil {
		t.Fatalf("подписан: %v", err)
	}
	// Второй раз абонент слышит «уже брали», а не про канал.
	ch.member = false
	if _, err := e.c.Trial(ctx, e.user.ID); !errors.Is(err, core.ErrTrialUsed) {
		t.Fatalf("повтор: ждали ErrTrialUsed, получили %v", err)
	}
}

func TestChannelErrorLetsThrough(t *testing.T) {
	ctx := context.Background()
	ch := &fakeChannels{err: errors.New("Bad Request: member list is inaccessible")}
	e := channelEnv(t, ch)
	if _, err := e.c.Trial(ctx, e.user.ID); err != nil {
		t.Fatalf("ошибка Telegram не пропустила абонента: %v", err)
	}
	other, err := e.c.Register(ctx, 2002, "", "ru")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.c.Trial(ctx, other.ID); err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, a := range adminTasks(t, e) {
		if a.Key == "admin.channel_check" {
			n++
			if a.Topic != core.TopicErrors {
				t.Fatalf("тревога не в теме ошибок: %s", a.Topic)
			}
		}
	}
	if n != 1 {
		t.Fatalf("тревог о канале за час %d, ждали 1", n)
	}
}

func TestNoChannelNoCheck(t *testing.T) {
	ch := &fakeChannels{}
	e := channelEnv(t, ch)
	if err := e.cfg.Set("trial.channel", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := e.c.Trial(context.Background(), e.user.ID); err != nil {
		t.Fatal(err)
	}
	if len(ch.asked) != 0 {
		t.Fatal("без канала в настройке Telegram всё равно спросили")
	}
}
