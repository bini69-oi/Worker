package core_test

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/bini69-oi/worker/core"
)

func userNotices(t *testing.T, e *env) []core.NotifyTask {
	t.Helper()
	var out []core.NotifyTask
	for _, task := range e.st.Tasks() {
		if task.Kind == core.KindNotifyUser {
			var n core.NotifyTask
			if err := json.Unmarshal(task.Payload, &n); err != nil {
				t.Fatal(err)
			}
			out = append(out, n)
		}
	}
	return out
}

func TestSendToUser(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	helperWith(t, e, 50, core.PermSupport)
	if err := e.c.SendToUser(ctx, 50, e.user.ID, "Проверили <b>всё</b> работает"); err != nil {
		t.Fatal(err)
	}
	got := userNotices(t, e)
	if len(got) != 1 || got[0].UserID != e.user.ID || got[0].Key != "notify.support_reply" {
		t.Fatalf("очередь: %+v", got)
	}
	if strings.Contains(got[0].Vars["text"], "<b>") || !strings.Contains(got[0].Vars["text"], "&lt;b&gt;") {
		t.Errorf("текст админа не экранирован для разметки бота: %q", got[0].Vars["text"])
	}
	audit := e.st.Audit()
	if last := audit[len(audit)-1]; last.Action != "user.message" || last.Target != strconv.FormatInt(e.user.ID, 10) {
		t.Errorf("журнал: %+v", last)
	}
	e.process(t)
	if len(e.notes.ToUsers) != 1 {
		t.Errorf("сообщение не ушло абоненту")
	}
}

func TestSendToUserRefusals(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	helperWith(t, e, 51, core.PermUsers) // есть право смотреть, но не писать
	if err := e.c.SendToUser(ctx, 51, e.user.ID, "привет"); !errors.Is(err, core.ErrForbidden) {
		t.Errorf("помощник без support: %v", err)
	}
	if n := len(userNotices(t, e)); n != 0 {
		t.Errorf("в очереди %d сообщений после отказа", n)
	}
	cases := []struct {
		name, text string
		want       error
	}{
		{"пусто", "  ", core.ErrInvalid},
		{"слишком длинный", strings.Repeat("я", 4097), core.ErrTooLong},
	}
	for _, c := range cases {
		if err := e.c.SendToUser(ctx, owner, e.user.ID, c.text); !errors.Is(err, c.want) {
			t.Errorf("%s: %v", c.name, err)
		}
	}
	if err := e.c.SendToUser(ctx, owner, 9999, "привет"); !errors.Is(err, core.ErrNotFound) {
		t.Errorf("нет абонента: %v", err)
	}
	if err := e.c.MarkUnreachable(ctx, e.user.ID); err != nil {
		t.Fatal(err)
	}
	if err := e.c.SendToUser(ctx, owner, e.user.ID, "привет"); !errors.Is(err, core.ErrUnreachable) {
		t.Errorf("абонент заблокировал бота: %v", err)
	}
}
