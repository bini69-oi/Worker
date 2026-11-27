package bot

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bini69-oi/worker/core"
	"github.com/bini69-oi/worker/core/coretest"
	"github.com/bini69-oi/worker/pay/gatewaytest"
	"github.com/bini69-oi/worker/pay/stars"
	"github.com/bini69-oi/worker/settings"
	"github.com/bini69-oi/worker/store"
	"github.com/bini69-oi/worker/texts"
	"github.com/bini69-oi/worker/tg"
)

var update = flag.Bool("update", false, "переписать эталоны экранов в testdata")

const (
	owner  = 1
	userTg = 1001
)

var t0 = time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

// call это один вызов Telegram, как его видит тест.
type call struct {
	method string
	chat   int64
	msg    int
	text   string
	markup *tg.InlineKeyboardMarkup
	alert  bool
}

// fakeAPI это Telegram, который записывает вызовы. editErr подменяет ответ на правку сообщения.
type fakeAPI struct {
	mu      sync.Mutex
	calls   []call
	nextMsg int
	editErr error
	sendErr error // ошибка на отправку нового сообщения
	member  string
}

func (f *fakeAPI) add(c call) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, c)
}

func (f *fakeAPI) SendMessage(_ context.Context, p tg.SendParams) (tg.Message, error) {
	f.mu.Lock()
	if f.sendErr != nil {
		defer f.mu.Unlock()
		return tg.Message{}, f.sendErr
	}
	f.nextMsg++
	id := f.nextMsg + 100
	f.mu.Unlock()
	f.add(call{method: "send", chat: p.ChatID, msg: id, text: p.Text, markup: p.Markup})
	return tg.Message{MessageID: id, Chat: tg.Chat{ID: p.ChatID}}, nil
}

func (f *fakeAPI) EditMessageText(_ context.Context, p tg.EditParams) error {
	f.add(call{method: "edit", chat: p.ChatID, msg: p.MessageID, text: p.Text, markup: p.Markup})
	return f.editErr
}

func (f *fakeAPI) EditMessageReplyMarkup(_ context.Context, chat int64, msg int, m *tg.InlineKeyboardMarkup) error {
	f.add(call{method: "edit_markup", chat: chat, msg: msg, markup: m})
	return nil
}

func (f *fakeAPI) AnswerCallbackQuery(_ context.Context, id, text string, alert bool) error {
	f.add(call{method: "answer", text: text, alert: alert})
	return nil
}

func (f *fakeAPI) CopyMessage(_ context.Context, to int64, thread int, from int64, msgID, replyTo int) (int, error) {
	f.mu.Lock()
	f.nextMsg++
	id := f.nextMsg + 100
	f.mu.Unlock()
	f.add(call{method: "copy", chat: to, msg: msgID, text: fmt.Sprintf("thread=%d from=%d reply=%d", thread, from, replyTo)})
	return id, nil
}

func (f *fakeAPI) SendInvoice(_ context.Context, p tg.InvoiceParams) (tg.Message, error) {
	f.add(call{method: "invoice", chat: p.ChatID, text: fmt.Sprintf("%s %s %v", p.Currency, p.Payload, p.Prices)})
	return tg.Message{}, nil
}

func (f *fakeAPI) CreateInvoiceLink(_ context.Context, p tg.InvoiceParams) (string, error) {
	f.add(call{method: "invoice_link", text: fmt.Sprintf("%s %s %v period=%d", p.Currency, p.Payload, p.Prices, p.SubscriptionPeriod)})
	return "https://t.me/$invoice", nil
}

func (f *fakeAPI) AnswerPreCheckoutQuery(_ context.Context, id string, ok bool, errMsg string) error {
	f.add(call{method: "precheckout", text: fmt.Sprintf("ok=%v %s", ok, errMsg)})
	return nil
}

func (f *fakeAPI) SetMyCommands(_ context.Context, cmds []tg.BotCommand, lang string) error {
	f.add(call{method: "commands", text: fmt.Sprintf("%s %v", lang, cmds)})
	return nil
}

func (f *fakeAPI) SetChatMenuButton(_ context.Context, chat int64, text, url string) error {
	f.add(call{method: "menu_button", chat: chat, text: text + " " + url})
	return nil
}

func (f *fakeAPI) CreateForumTopic(_ context.Context, chat int64, name string) (int, error) {
	f.mu.Lock()
	f.nextMsg++
	id := f.nextMsg + 100
	f.mu.Unlock()
	f.add(call{method: "topic", chat: chat, msg: id, text: name})
	return id, nil
}

func (f *fakeAPI) GetChatMember(_ context.Context, chat string, user int64) (string, error) {
	return f.member, nil
}

func (f *fakeAPI) LeaveChat(_ context.Context, chat int64) error {
	f.add(call{method: "leave", chat: chat})
	return nil
}

// take отдаёт вызовы с прошлого take.
func (f *fakeAPI) take() []call {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := f.calls
	f.calls = nil
	return out
}

// render пишет вызовы так, как их сравнивают с эталоном: метод, текст и кнопки рядами.
func render(calls []call) string {
	var sb strings.Builder
	for _, c := range calls {
		fmt.Fprintf(&sb, "== %s chat=%d msg=%d", c.method, c.chat, c.msg)
		if c.alert {
			sb.WriteString(" alert")
		}
		sb.WriteString("\n")
		if c.text != "" {
			sb.WriteString(c.text + "\n")
		}
		if c.markup != nil {
			for _, row := range c.markup.InlineKeyboard {
				for i, btn := range row {
					if i > 0 {
						sb.WriteString(" | ")
					}
					sb.WriteString("[" + btn.Text)
					switch {
					case btn.CallbackData != "":
						sb.WriteString(" → " + btn.CallbackData)
					case btn.URL != "":
						sb.WriteString(" ↗ " + btn.URL)
					case btn.CopyText != nil:
						sb.WriteString(" ⧉ " + btn.CopyText.Text)
					case btn.WebApp != nil:
						sb.WriteString(" ▣ " + btn.WebApp.URL)
					}
					sb.WriteString("]")
				}
				sb.WriteString("\n")
			}
		}
	}
	return sb.String()
}

// golden сравнивает вызовы с эталоном testdata/<name>.golden. С флагом -update переписывает эталон.
func golden(t *testing.T, name string, calls []call) {
	t.Helper()
	got := render(calls)
	path := filepath.Join("testdata", name+".golden")
	if *update {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("нет эталона %s: запустите go test ./bot -update и проверьте дифф", path)
	}
	if string(want) != got {
		t.Fatalf("экран %s не совпал с эталоном.\nждали:\n%s\nполучили:\n%s", name, want, got)
	}
}

// env это бот на настоящем ядре и SQLite во временном файле, с фейковыми Telegram и панелью.
type env struct {
	b      *Bot
	api    *fakeAPI
	c      *core.Core
	cfg    *settings.Settings
	tx     *texts.Texts
	clock  *coretest.Clock
	panel  *coretest.Panel
	db     *store.DB
	fake   *gatewaytest.Fake
	notify *lazyNotifier
	upd    int64
	step   time.Duration // часы сдвигаются перед каждым действием, чтобы тесты не упирались во флуд
}

func newEnv(t *testing.T) *env {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "worker.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	e := &env{api: &fakeAPI{member: "member"}, clock: coretest.NewClock(t0), panel: coretest.NewPanel(), db: db, step: time.Second}
	key := []byte(strings.Repeat("k", 32))
	if e.cfg, err = settings.New(store.NewSettingsStore(db), key, e.clock.Now); err != nil {
		t.Fatal(err)
	}
	if e.tx, err = texts.New(store.NewTextsStore(db), func() string { return "ru" }, e.clock.Now); err != nil {
		t.Fatal(err)
	}
	e.fake = &gatewaytest.Fake{Secret: "s"}
	e.notify = &lazyNotifier{}
	e.c = core.New(core.Deps{Store: store.NewCoreStore(db), Clock: e.clock, Config: e.cfg, Panel: e.panel,
		Notifier: e.notify, OwnerID: owner, Channels: Channels{API: e.api},
		Gateways: []core.Gateway{stars.New(e.cfg), e.fake}})
	e.b = New(Options{Core: e.c, Texts: e.tx, API: e.api, Config: e.cfg, Now: e.clock.Now,
		Name: "workerbot", URL: "https://vpn.example", Owner: owner, DLKey: []byte(strings.Repeat("d", 32))})
	e.notify.b = e.b
	return e
}

func (e *env) set(t *testing.T, kv ...string) {
	t.Helper()
	for i := 0; i+1 < len(kv); i += 2 {
		if err := e.cfg.Set(kv[i], kv[i+1]); err != nil {
			t.Fatal(err)
		}
	}
}

func (e *env) next() int64 {
	e.clock.Add(e.step)
	e.upd++
	return e.upd
}

// text шлёт боту сообщение от абонента в личке.
func (e *env) text(t *testing.T, from int64, text string) {
	t.Helper()
	m := &tg.Message{MessageID: int(e.next()), From: &tg.User{ID: from, FirstName: "Иван", Username: "ivan", LanguageCode: "ru"},
		Chat: tg.Chat{ID: from, Type: "private"}, Text: text}
	if err := e.b.handle(context.Background(), tg.Update{UpdateID: e.upd, Message: m}); err != nil {
		t.Fatal(err)
	}
}

// press нажимает кнопку в сообщении msg.
func (e *env) press(t *testing.T, from int64, msg int, data string) {
	t.Helper()
	q := &tg.CallbackQuery{ID: fmt.Sprint(e.next()), From: tg.User{ID: from, FirstName: "Иван", LanguageCode: "ru"},
		Message: &tg.Message{MessageID: msg, Chat: tg.Chat{ID: from, Type: "private"}}, Data: data}
	if err := e.b.handle(context.Background(), tg.Update{UpdateID: e.upd, CallbackQuery: q}); err != nil {
		t.Fatal(err)
	}
}

// start шлёт /start и отдаёт абонента.
func (e *env) start(t *testing.T) core.User {
	t.Helper()
	e.text(t, userTg, "/start")
	u, err := e.c.UserByTg(context.Background(), userTg)
	if err != nil {
		t.Fatal(err)
	}
	e.api.take()
	return u
}

func (e *env) tariff(t *testing.T, name string, periods ...core.Period) core.Tariff {
	t.Helper()
	tr, err := e.c.SaveTariff(context.Background(), owner, core.Tariff{Name: name, Active: true, Periods: periods,
		TrafficBytes: 100 << 30, Devices: 3, Squads: []string{"a"}})
	if err != nil {
		t.Fatal(err)
	}
	return tr
}

func (e *env) topup(t *testing.T, userID, amount int64) {
	t.Helper()
	if _, err := e.c.AdminAdjust(context.Background(), owner, userID, amount, "тест"); err != nil {
		t.Fatal(err)
	}
}

// sync прогоняет очередь ядра: панель создаёт абонента и выдаёт ссылку подписки.
func (e *env) sync(t *testing.T) {
	t.Helper()
	if _, err := e.c.ProcessDue(context.Background()); err != nil {
		t.Fatal(err)
	}
}

// paid шлёт боту сообщение об оплате Stars от абонента.
func (e *env) paid(t *testing.T, from int64, sp *tg.SuccessfulPayment, rp *tg.RefundedPayment) {
	t.Helper()
	m := &tg.Message{MessageID: int(e.next()), From: &tg.User{ID: from}, Chat: tg.Chat{ID: from, Type: "private"},
		SuccessfulPayment: sp, RefundedPayment: rp}
	if err := e.b.handle(context.Background(), tg.Update{UpdateID: e.upd, Message: m}); err != nil {
		t.Fatal(err)
	}
}

func (e *env) precheckout(t *testing.T, from int64, currency, payload string, amount int64) string {
	t.Helper()
	q := &tg.PreCheckoutQuery{ID: "pc", From: tg.User{ID: from}, Currency: currency, TotalAmount: amount, InvoicePayload: payload}
	if err := e.b.handle(context.Background(), tg.Update{UpdateID: e.next(), PreCheckoutQuery: q}); err != nil {
		t.Fatal(err)
	}
	calls := e.api.take()
	return calls[len(calls)-1].text
}

func (e *env) balance(t *testing.T) int64 {
	t.Helper()
	u, err := e.c.UserByTg(context.Background(), userTg)
	if err != nil {
		t.Fatal(err)
	}
	return u.Balance
}

// lazyNotifier отдаёт уведомления ядра боту, который собирается после ядра.
type lazyNotifier struct{ b *Bot }

func (l *lazyNotifier) User(ctx context.Context, n core.NotifyTask) error { return l.b.User(ctx, n) }
func (l *lazyNotifier) Admin(ctx context.Context, n core.AdminTask) error { return l.b.Admin(ctx, n) }
func (l *lazyNotifier) Owner(ctx context.Context, n core.AdminTask) error { return l.b.Owner(ctx, n) }
func (l *lazyNotifier) Confirm(ctx context.Context, n core.ConfirmTask) error {
	return l.b.Confirm(ctx, n)
}

// group шлёт сообщение в группу от человека from.
func (e *env) group(t *testing.T, chat, from int64, forum bool, text string, reply *tg.Message) {
	t.Helper()
	m := &tg.Message{MessageID: int(e.next()), From: &tg.User{ID: from, FirstName: "Админ"}, Chat: tg.Chat{ID: chat, Type: "supergroup", IsForum: forum},
		Text: text, ReplyToMessage: reply}
	if err := e.b.handle(context.Background(), tg.Update{UpdateID: e.upd, Message: m}); err != nil {
		t.Fatal(err)
	}
}

// pressIn нажимает кнопку в чате chat (группа или личка админа).
func (e *env) pressIn(t *testing.T, chat, from int64, msg int, data string) {
	t.Helper()
	typ := "supergroup"
	if chat > 0 {
		typ = "private"
	}
	q := &tg.CallbackQuery{ID: fmt.Sprint(e.next()), From: tg.User{ID: from}, Message: &tg.Message{MessageID: msg, Chat: tg.Chat{ID: chat, Type: typ}}, Data: data}
	if err := e.b.handle(context.Background(), tg.Update{UpdateID: e.upd, CallbackQuery: q}); err != nil {
		t.Fatal(err)
	}
}

// bind привязывает группу chat с темами.
func (e *env) bind(t *testing.T, chat int64) {
	t.Helper()
	code, err := e.c.NewChatCode(context.Background(), owner)
	if err != nil {
		t.Fatal(err)
	}
	e.group(t, chat, owner, true, "/bind "+code, nil)
	e.api.take()
}

// groupIn шлёт сообщение в тему thread группы. reply 0 значит обычное сообщение в теме:
// Telegram ставит реплай на первое сообщение темы.
func (e *env) groupIn(t *testing.T, chat, from int64, thread, reply int, text string) {
	t.Helper()
	if reply == 0 {
		reply = thread
	}
	m := &tg.Message{MessageID: int(e.next()), From: &tg.User{ID: from}, Chat: tg.Chat{ID: chat, Type: "supergroup", IsForum: true},
		ThreadID: thread, IsTopicMessage: true, Text: text, ReplyToMessage: &tg.Message{MessageID: reply}}
	if err := e.b.handle(context.Background(), tg.Update{UpdateID: e.upd, Message: m}); err != nil {
		t.Fatal(err)
	}
}
