package bot

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/bini69-oi/worker/core"
	"github.com/bini69-oi/worker/tg"
)

func supportEnv(t *testing.T) (*env, int) {
	e := newEnv(t)
	e.start(t)
	e.bind(t, chat)
	ch, _ := e.c.AdminChat(context.Background())
	return e, int(ch.Topics[core.TopicSupport])
}

func TestSupportCardOncePerHour(t *testing.T) {
	e, _ := supportEnv(t)
	for _, text := range []string{"не подключается", "на айфоне", "спасибо"} {
		e.text(t, userTg, text)
	}
	calls := e.api.take()
	cards, copies := 0, 0
	for _, c := range calls {
		if c.method == "send" && c.chat == chat && strings.Contains(c.text, "Тариф:") {
			cards++
		}
		if c.method == "copy" && c.chat == chat {
			copies++
		}
	}
	if cards != 1 || copies != 3 {
		t.Fatalf("три сообщения: карточек %d, копий %d\n%s", cards, copies, render(calls))
	}
	if !strings.Contains(render(calls), "Отправлено в поддержку") {
		t.Fatal("абонент не видит, что сообщение ушло")
	}
}

func TestSupportReply(t *testing.T) {
	e, thread := supportEnv(t)
	e.text(t, userTg, "не подключается")
	e.api.take()
	link := e.lastSupportLink(t)

	e.groupIn(t, chat, owner, thread, link, "Перезапустите приложение")
	if got := render(e.api.take()); !strings.Contains(got, "== copy chat=1001") {
		t.Fatalf("ответ реплаем не ушёл абоненту: %s", got)
	}
	e.groupIn(t, chat, owner, thread, 0, "коллеги, кто возьмёт?")
	if c := e.api.take(); len(c) != 0 {
		t.Fatalf("сообщение без реплая куда-то ушло: %s", render(c))
	}
	e.groupIn(t, chat, 555, thread, link, "я не админ")
	if c := e.api.take(); len(c) != 0 {
		t.Fatalf("посторонний ответил абоненту: %s", render(c))
	}
	e.groupIn(t, chat, owner, thread, 99999, "ответ на неизвестное")
	if got := render(e.api.take()); !strings.Contains(got, "слишком старое") {
		t.Fatalf("реплай на старое: %s", got)
	}
}

// lastSupportLink ищет номер последнего сообщения в группе, связанного с абонентом.
func (e *env) lastSupportLink(t *testing.T) int {
	t.Helper()
	u, _ := e.c.UserByTg(context.Background(), userTg)
	for id := e.api.nextMsg + 100; id > 100; id-- {
		if uid, err := e.c.SupportUser(context.Background(), chat, int64(id)); err == nil && uid == u.ID {
			return id
		}
	}
	t.Fatal("связь поддержки не записана")
	return 0
}

type blockedCopyAPI struct{ *fakeAPI }

func (b blockedCopyAPI) CopyMessage(ctx context.Context, to int64, thread int, from int64, msg, reply int) (int, error) {
	if to > 0 {
		return 0, &tg.Error{Code: 403, Description: "Forbidden: bot was blocked by the user"}
	}
	return b.fakeAPI.CopyMessage(ctx, to, thread, from, msg, reply)
}

func TestSupportUserBlocked(t *testing.T) {
	e, thread := supportEnv(t)
	e.text(t, userTg, "вопрос")
	e.api.take()
	link := e.lastSupportLink(t)
	e.b.api = blockedCopyAPI{e.api}
	e.groupIn(t, chat, owner, thread, link, "ответ")
	if got := render(e.api.take()); !strings.Contains(got, "заблокировал бота") {
		t.Fatalf("блокировка не замечена: %s", got)
	}
}

func TestSupportLimit(t *testing.T) {
	e, _ := supportEnv(t)
	for i := range 40 {
		e.text(t, userTg, "сообщение "+itoa(int64(i)))
	}
	copies, warns := 0, 0
	for _, c := range e.api.take() {
		if c.method == "copy" {
			copies++
		}
		if strings.Contains(c.text, "Слишком много сообщений") {
			warns++
		}
	}
	if copies != 30 || warns != 1 {
		t.Fatalf("40 сообщений: копий %d, предупреждений %d", copies, warns)
	}
}

func TestSupportWithoutGroup(t *testing.T) {
	e := newEnv(t)
	e.set(t, "support.contact", "@help")
	e.start(t)
	e.text(t, userTg, "помогите")
	if got := render(e.api.take()); !strings.Contains(got, "@help") || strings.Contains(got, "copy") {
		t.Fatalf("без группы: %s", got)
	}
}

func TestSupportCleanup(t *testing.T) {
	e, thread := supportEnv(t)
	e.text(t, userTg, "вопрос")
	e.api.take()
	link := e.lastSupportLink(t)
	e.clock.Add(31 * 24 * time.Hour)
	if n, err := e.c.CleanSupport(context.Background()); err != nil || n == 0 {
		t.Fatalf("очистка: %d %v", n, err)
	}
	e.groupIn(t, chat, owner, thread, link, "поздний ответ")
	if got := render(e.api.take()); !strings.Contains(got, "слишком старое") {
		t.Fatalf("реплай после очистки: %s", got)
	}
}

func TestSupportTopicDeleted(t *testing.T) {
	e, _ := supportEnv(t)
	e.b.api = &goneTopicAPI{fakeAPI: e.api}
	e.text(t, userTg, "первое сообщение")
	got := render(e.api.take())
	if !strings.Contains(got, "== topic") || !strings.Contains(got, "Тариф:") || !strings.Contains(got, "== copy chat=-100500") {
		t.Fatalf("после удаления темы сообщение не дошло с карточкой: %s", got)
	}
}
