package bot

import (
	"context"
	"strings"
	"testing"

	"github.com/bini69-oi/worker/core"
	"github.com/bini69-oi/worker/tg"
)

const chat = -100500

func TestUserNotices(t *testing.T) {
	e := newEnv(t)
	u := e.start(t)
	keys := map[string]map[string]string{
		"notify.remind": {"days": "3"}, "notify.expired": {"trial": "0"}, "notify.bought": {"days": "30", "until": "31.01.2026"},
		"notify.topup": {"amount": "30000"}, "notify.autorenew_ok": {"until": "31.01.2026"}, "notify.autorenew_fail": {"missing": "15000"},
		"notify.autorenew_off": nil, "notify.recurring_failed": {"amount": "30000"}, "notify.traffic_limited": nil,
		"notify.ref_bonus": {"amount": "2500"}, "notify.pay_expired": {"amount": "30000"}, "notify.pay_review": nil, "notify.refunded": {"amount": "30000"},
	}
	var all []call
	for _, k := range []string{"notify.remind", "notify.expired", "notify.bought", "notify.topup", "notify.autorenew_ok", "notify.autorenew_fail",
		"notify.autorenew_off", "notify.recurring_failed", "notify.traffic_limited", "notify.ref_bonus", "notify.pay_expired", "notify.pay_review", "notify.refunded"} {
		if err := e.b.User(context.Background(), core.NotifyTask{UserID: u.ID, Key: k, Vars: keys[k]}); err != nil {
			t.Fatal(err)
		}
		all = append(all, e.api.take()...)
	}
	golden(t, "user_notices", all)
}

// blockedAPI отвечает «бот заблокирован» на отправку.
type blockedAPI struct{ *fakeAPI }

func (b blockedAPI) SendMessage(context.Context, tg.SendParams) (tg.Message, error) {
	return tg.Message{}, &tg.Error{Code: 403, Description: "Forbidden: bot was blocked by the user"}
}

func TestBlockedMarksUnreachable(t *testing.T) {
	e := newEnv(t)
	u := e.start(t)
	e.b.api = blockedAPI{e.api}
	if err := e.b.User(context.Background(), core.NotifyTask{UserID: u.ID, Key: "notify.remind", Vars: map[string]string{"days": "3"}}); err != nil {
		t.Fatalf("блокировка должна закрывать задачу, а не повторять: %v", err)
	}
	if got, _ := e.c.User(context.Background(), u.ID); got.UnreachableAt.IsZero() {
		t.Fatal("абонент не отмечен недоступным")
	}
	e.b.api = e.api
	e.text(t, userTg, "/start")
	if got, _ := e.c.User(context.Background(), u.ID); !got.UnreachableAt.IsZero() {
		t.Fatal("вернулся по /start, а отметка осталась")
	}
}

func TestBindGroup(t *testing.T) {
	e := newEnv(t)
	code, _ := e.c.NewChatCode(context.Background(), owner)
	e.group(t, chat, owner, false, "/bind "+code, nil)
	if got := render(e.api.take()); !strings.Contains(got, "Включите в группе темы") {
		t.Fatalf("группа без тем: %s", got)
	}
	e.group(t, chat, 777, true, "/bind "+code, nil)
	if got := render(e.api.take()); !strings.Contains(got, "Код не подошёл") {
		t.Fatalf("чужой привязал: %s", got)
	}
	e.group(t, chat, owner, true, "/bind "+code, nil)
	golden(t, "bind", e.api.take())
	ch, err := e.c.AdminChat(context.Background())
	if err != nil || ch.ChatID != chat || len(ch.Topics) != 7 {
		t.Fatalf("привязка: %+v %v", ch, err)
	}
}

func TestLeaveForeignGroup(t *testing.T) {
	e := newEnv(t)
	join := func(id int64) {
		u := &tg.ChatMemberUpdated{Chat: tg.Chat{ID: id, Type: "supergroup"}, OldChatMember: tg.ChatMember{Status: "left"}, NewChatMember: tg.ChatMember{Status: "member"}}
		if err := e.b.handle(context.Background(), tg.Update{UpdateID: e.next(), MyChatMember: u}); err != nil {
			t.Fatal(err)
		}
	}
	join(-1)
	join(chat)
	e.bind(t, chat)
	e.clock.Add(11 * 60e9)
	e.b.sweepGroups(context.Background())
	calls := e.api.take()
	if len(calls) != 1 || calls[0].method != "leave" || calls[0].chat != -1 {
		t.Fatalf("выход из чужой группы: %s", render(calls))
	}
}

func TestEventsToTopics(t *testing.T) {
	e := newEnv(t)
	u := e.start(t)
	e.bind(t, chat)
	ch, _ := e.c.AdminChat(context.Background())
	if err := e.b.Admin(context.Background(), core.AdminTask{Topic: core.TopicPayments, Key: "admin.payment",
		Vars: map[string]string{"user_id": itoa(u.ID), "amount": "30000", "gateway": "stars"}}); err != nil {
		t.Fatal(err)
	}
	calls := e.api.take()
	golden(t, "event_payment", calls)
	if calls[0].chat != chat || int64(calls[0].msg) == 0 {
		t.Fatalf("не в группу: %s", render(calls))
	}
	_ = ch

	// Тема выключена: ничего.
	e.set(t, "notify.new", "false")
	_ = e.b.Admin(context.Background(), core.AdminTask{Topic: core.TopicNew, Key: "admin.new_user", Vars: map[string]string{"user_id": itoa(u.ID)}})
	if c := e.api.take(); len(c) != 0 {
		t.Fatalf("выключенная тема получила событие: %s", render(c))
	}
	if err := e.cfg.Set("notify.security", "false"); err == nil {
		t.Fatal("тему безопасности выключили")
	}
}

// goneTopicAPI отвечает «тема не найдена» на первую отправку в тему.
type goneTopicAPI struct {
	*fakeAPI
	failed bool
}

func (g *goneTopicAPI) SendMessage(ctx context.Context, p tg.SendParams) (tg.Message, error) {
	if p.ThreadID != 0 && !g.failed {
		g.failed = true
		return tg.Message{}, &tg.Error{Code: 400, Description: "Bad Request: message thread not found"}
	}
	return g.fakeAPI.SendMessage(ctx, p)
}

func TestTopicRecreated(t *testing.T) {
	e := newEnv(t)
	e.bind(t, chat)
	g := &goneTopicAPI{fakeAPI: e.api}
	e.b.api = g
	if err := e.b.Admin(context.Background(), core.AdminTask{Topic: core.TopicErrors, Key: "admin.panel_diff", Vars: map[string]string{"count": "2"}}); err != nil {
		t.Fatal(err)
	}
	got := render(e.api.take())
	if !strings.Contains(got, "== topic") || !strings.Contains(got, "расхождения: 2") {
		t.Fatalf("тема не пересоздана: %s", got)
	}
}

func TestSecurityToOwner(t *testing.T) {
	e := newEnv(t)
	task := core.AdminTask{Topic: core.TopicSecurity, Key: "admin.login_bruteforce", Vars: map[string]string{"admin": "1", "ip": "1.2.3.4"}}
	if err := e.b.Admin(context.Background(), task); err != nil {
		t.Fatal(err)
	}
	if c := e.api.take(); len(c) != 1 || c[0].chat != owner {
		t.Fatalf("без группы тревога не у владельца: %s", render(c))
	}
	e.bind(t, chat)
	if err := e.b.Admin(context.Background(), task); err != nil {
		t.Fatal(err)
	}
	c := e.api.take()
	if len(c) != 2 || c[0].chat != owner || c[1].chat != chat {
		t.Fatalf("с группой тревога должна быть и в теме, и у владельца: %s", render(c))
	}
}

func TestAdminButtons(t *testing.T) {
	e := newEnv(t)
	u := e.start(t)
	e.bind(t, chat)
	ban := "adm:ban:" + itoa(u.ID) + ":p"
	e.pressIn(t, chat, 555, 9, ban)
	if got := render(e.api.take()); !strings.Contains(got, "Нет прав") {
		t.Fatalf("посторонний забанил: %s", got)
	}
	if got, _ := e.c.User(context.Background(), u.ID); !got.BannedAt.IsZero() {
		t.Fatal("бан прошёл без прав")
	}
	e.pressIn(t, chat, owner, 9, ban)
	got := render(e.api.take())
	if !strings.Contains(got, "adm:unban:"+itoa(u.ID)+":p") {
		t.Fatalf("кнопка не сменилась на «Разбанить»: %s", got)
	}
	if u2, _ := e.c.User(context.Background(), u.ID); u2.BannedAt.IsZero() {
		t.Fatal("бан не прошёл")
	}
}

func TestConfirmButtons(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	if err := e.c.SetOwnerPassword(ctx, "owner-password-1"); err != nil {
		t.Fatal(err)
	}
	st, err := e.c.LoginStart(ctx, owner, "owner-password-1", core.LoginInfo{IP: "1.2.3.4", Agent: "Firefox"})
	if err != nil {
		t.Fatal(err)
	}
	e.sync(t)
	calls := e.api.take()
	if len(calls) == 0 || !strings.Contains(render(calls), "Вход в админку") {
		t.Fatalf("сообщение о входе: %s", render(calls))
	}
	msg := calls[len(calls)-1].msg
	e.pressIn(t, owner+1000, owner+1000, msg, "lg:"+itoa(st.ConfirmID)+":"+st.Code)
	if got := render(e.api.take()); !strings.Contains(got, "Нет прав") {
		t.Fatalf("нажал чужой: %s", got)
	}
	e.pressIn(t, owner, owner, msg, "lg:"+itoa(st.ConfirmID)+":"+st.Code)
	if got := render(e.api.take()); !strings.Contains(got, "Вход подтверждён") {
		t.Fatalf("верный код: %s", got)
	}

	st, _ = e.c.LoginStart(ctx, owner, "owner-password-1", core.LoginInfo{IP: "1.2.3.4", Agent: "Firefox"})
	e.sync(t)
	e.api.take()
	wrong := "99"
	if st.Code == wrong {
		wrong = "98"
	}
	e.pressIn(t, owner, owner, msg, "lg:"+itoa(st.ConfirmID)+":"+wrong)
	if got := render(e.api.take()); !strings.Contains(got, "Вход отклонён") {
		t.Fatalf("неверный код: %s", got)
	}
}
