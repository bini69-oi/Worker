package bot

import (
	"context"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/bini69-oi/worker/core"
)

var twoDigits = regexp.MustCompile(`^wl:(\d+):(\d\d)$`)

// startWeb начинает вход на сайте и открывает ссылку в боте от имени абонента. Отдаёт ответ бота, код сайта
// и номера кнопок: верной, неверной и «Это не я».
func startWeb(t *testing.T, e *env) (calls []call, st core.WebLoginStarted, right, wrong string) {
	t.Helper()
	var err error
	st, err = e.c.WebLoginStart(context.Background(), core.LoginInfo{IP: "203.0.113.7", Agent: "Firefox <b>"})
	if err != nil {
		t.Fatal(err)
	}
	e.text(t, userTg, "/start web_"+st.Nonce)
	calls = e.api.take()
	for _, c := range calls {
		if c.markup == nil {
			continue
		}
		for _, row := range c.markup.InlineKeyboard {
			for i, b := range row {
				m := twoDigits.FindStringSubmatch(b.CallbackData)
				switch {
				case m == nil:
				case m[2] == st.Code:
					right = b.CallbackData
				default:
					wrong = b.CallbackData
				}
				// Коды случайные: для эталона они заменяются на NN, «Это не я» остаётся как есть.
				if m != nil {
					row[i].Text, row[i].CallbackData = "NN", "wl:"+m[1]+":NN"
				}
			}
		}
	}
	return calls, st, right, wrong
}

func TestWebLoginPrompt(t *testing.T) {
	e := newEnv(t)
	calls, _, right, wrong := startWeb(t, e)
	if right == "" || wrong == "" {
		t.Fatalf("в сообщении нет кнопок с кодами: %s", render(calls))
	}
	golden(t, "web_login", calls)
}

func TestWebLoginRight(t *testing.T) {
	e := newEnv(t)
	_, st, right, _ := startWeb(t, e)
	e.press(t, userTg, 101, right)
	golden(t, "web_login_ok", e.api.take())
	res, err := e.c.WebLoginPoll(context.Background(), st.ID, core.LoginInfo{IP: "203.0.113.7"})
	if err != nil || res.State != core.LoginConfirmed {
		t.Fatalf("сайт после верного кода: %+v %v", res, err)
	}
}

func TestWebLoginWrongAndNotMe(t *testing.T) {
	for name, pick := range map[string]func(right, wrong string) string{
		"неверный код": func(_, wrong string) string { return wrong },
		"это не я": func(right, _ string) string {
			return strings.TrimSuffix(right, right[strings.LastIndex(right, ":")+1:]) + "no"
		},
	} {
		e := newEnv(t)
		_, st, right, wrong := startWeb(t, e)
		e.press(t, userTg, 101, pick(right, wrong))
		if got := render(e.api.take()); !strings.Contains(got, "Вход отклонён") {
			t.Fatalf("%s: %s", name, got)
		}
		if res, _ := e.c.WebLoginPoll(context.Background(), st.ID, core.LoginInfo{IP: "1.1.1.1"}); res.State != core.LoginRejected {
			t.Fatalf("%s: сайт видит %+v", name, res)
		}
	}
}

func TestWebLoginExpiredAndUsed(t *testing.T) {
	e := newEnv(t)
	st, err := e.c.WebLoginStart(context.Background(), core.LoginInfo{IP: "203.0.113.7"})
	if err != nil {
		t.Fatal(err)
	}
	e.clock.Add(6 * time.Minute)
	e.text(t, userTg, "/start web_"+st.Nonce)
	if got := render(e.api.take()); !strings.Contains(got, "Ссылка устарела") || !strings.Contains(got, "E6041") {
		t.Fatalf("устаревшая ссылка: %s", got)
	}

	_, st2, right, _ := startWeb(t, e)
	e.press(t, userTg, 101, right)
	e.api.take()
	e.text(t, userTg, "/start web_"+st2.Nonce)
	if got := render(e.api.take()); !strings.Contains(got, "Ссылка уже использована") {
		t.Fatalf("использованная ссылка: %s", got)
	}
}

func TestStartArgWeb(t *testing.T) {
	nonce := strings.Repeat("aB-_", 10) + "abc" // 43 знака, как у настоящего nonce
	if k, v := startArg("web_" + nonce); k != "web" || v != nonce {
		t.Fatalf("web_: %q %q", k, v)
	}
	for _, bad := range []string{"web_", "web_" + strings.Repeat("a", 61), "web_ab cd", "web_ab;c"} {
		if k, _ := startArg(bad); k != "" {
			t.Errorf("%q принят как %q", bad, k)
		}
	}
}

func FuzzStartArgWeb(f *testing.F) {
	f.Add("web_abc")
	f.Add("web_" + strings.Repeat("-", 60))
	f.Fuzz(func(t *testing.T, s string) {
		if k, v := startArg(s); k == "web" && (len(v) == 0 || len(s) > 64 || strings.ContainsAny(v, " :;/")) {
			t.Fatalf("принято %q", s)
		}
	})
}
