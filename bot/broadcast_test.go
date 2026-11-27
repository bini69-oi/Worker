package bot

import (
	"context"
	"errors"
	"testing"

	"github.com/bini69-oi/worker/core"
	"github.com/bini69-oi/worker/tg"
)

func TestBroadcastSend(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	if err := e.b.Send(ctx, 777, "<b>Привет</b>", "Открыть", "https://example.com"); err != nil {
		t.Fatal(err)
	}
	got := e.api.take()
	if len(got) != 1 || got[0].chat != 777 || got[0].text != "<b>Привет</b>" ||
		got[0].markup == nil || got[0].markup.InlineKeyboard[0][0].URL != "https://example.com" {
		t.Fatalf("ушло: %+v", got)
	}
	if err := e.b.Send(ctx, 777, "без кнопки", "", ""); err != nil {
		t.Fatal(err)
	}
	if got = e.api.take(); got[0].markup != nil {
		t.Errorf("кнопка без просьбы: %+v", got[0].markup)
	}

	cases := []struct {
		name string
		err  error
		want func(error) bool
	}{
		{"заблокировал бота", &tg.Error{Code: 403, Description: "Forbidden: bot was blocked by the user"},
			func(err error) bool { return errors.Is(err, core.ErrRecipientGone) }},
		{"удалил аккаунт", &tg.Error{Code: 403, Description: "Forbidden: user is deactivated"},
			func(err error) bool { return errors.Is(err, core.ErrRecipientGone) }},
		{"429", &tg.Error{Code: 429, Description: "Too Many Requests", RetryAfter: 9},
			func(err error) bool { var r *core.RetryAfter; return errors.As(err, &r) && r.Seconds == 9 }},
		{"прочее", &tg.Error{Code: 400, Description: "Bad Request: chat not found"},
			func(err error) bool { return err != nil && !errors.Is(err, core.ErrRecipientGone) }},
	}
	for _, c := range cases {
		e.api.sendErr = c.err
		if err := e.b.Send(ctx, 777, "x", "", ""); !c.want(err) {
			t.Errorf("%s: %v", c.name, err)
		}
	}
}
