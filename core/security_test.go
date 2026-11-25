package core_test

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bini69-oi/worker/core"
	"github.com/bini69-oi/worker/settings"
)

const (
	helper  = 2
	ownerPw = "owner-password-1"
	helpPw  = "helper-password-1"
	info    = "1.2.3.4"
)

// fakeTG это Telegram для смены токена: принимает только токен good.
type fakeTG struct{ webhook string }

func (f *fakeTG) CheckToken(_ context.Context, token string) error {
	if token != "123:good" {
		return errors.New("Unauthorized")
	}
	return nil
}

func (f *fakeTG) SetWebhook(_ context.Context, token string) error {
	f.webhook = token
	return nil
}

type secEnv struct {
	*env
	tg *fakeTG
}

func newSecEnv(t *testing.T) *secEnv {
	t.Helper()
	e := &secEnv{env: newEnv(t), tg: &fakeTG{}}
	e.c = core.New(core.Deps{Store: e.st, Clock: e.clock, Config: e.cfg, Panel: e.panel, Notifier: e.notes,
		Telegram: e.tg, OwnerID: owner})
	if err := e.c.SetOwnerPassword(context.Background(), ownerPw); err != nil {
		t.Fatal(err)
	}
	return e
}

// lastConfirm отдаёт последнее подтверждение, которое ушло в Telegram.
func (e *secEnv) lastConfirm(t *testing.T) core.ConfirmTask {
	t.Helper()
	e.process(t)
	if len(e.notes.ToConfirm) == 0 {
		t.Fatal("подтверждение в Telegram не ушло")
	}
	return e.notes.ToConfirm[len(e.notes.ToConfirm)-1]
}

func (e *secEnv) approve(t *testing.T, id int64) {
	t.Helper()
	if err := e.c.AnswerConfirm(context.Background(), id, owner, true); err != nil {
		t.Fatal(err)
	}
}

func (e *secEnv) addHelper(t *testing.T, perms ...core.Perm) {
	t.Helper()
	e.addHelperTg(t, helper, perms...)
}

func (e *secEnv) addHelperTg(t *testing.T, tg int64, perms ...core.Perm) {
	t.Helper()
	id, err := e.c.RequestAddHelper(context.Background(), owner, tg, perms, helpPw)
	if err != nil {
		t.Fatal(err)
	}
	e.approve(t, id)
}

// login проходит оба шага входа и отдаёт токен сессии.
func (e *secEnv) login(t *testing.T, tg int64, pw string) string {
	t.Helper()
	ctx := context.Background()
	st, err := e.c.LoginStart(ctx, tg, pw, core.LoginInfo{IP: info, Agent: "Firefox"})
	if err != nil {
		t.Fatal(err)
	}
	id, nonce := st.ConfirmID, st.Nonce
	if err := e.c.AnswerLogin(ctx, id, tg, st.Code); err != nil {
		t.Fatal(err)
	}
	token, _, err := e.c.LoginFinish(ctx, id, nonce, core.LoginInfo{IP: info})
	if err != nil {
		t.Fatal(err)
	}
	return token
}

func alerts(e *env, key string) int {
	n := 0
	for _, a := range e.notes.ToAdmins {
		if a.Key == key && a.Topic == core.TopicSecurity {
			n++
		}
	}
	return n
}

func TestHelperPerms(t *testing.T) {
	e := newSecEnv(t)
	ctx := context.Background()
	e.addHelper(t, core.PermStats)
	if _, err := e.c.AdminAdjust(ctx, helper, e.user.ID, 100, "подарок"); !errors.Is(err, core.ErrForbidden) {
		t.Fatalf("помощник без права выдавать деньги: ждали ErrForbidden, получили %v", err)
	}
	if _, err := e.c.RequestSecret(ctx, helper, "yookassa.secret_key", "live_x"); !errors.Is(err, core.ErrOwnerOnly) {
		t.Fatalf("помощник меняет ключ кассы: ждали ErrOwnerOnly, получили %v", err)
	}
	if _, err := e.c.RequestAddHelper(ctx, helper, 3, nil, "password-123"); !errors.Is(err, core.ErrOwnerOnly) {
		t.Fatalf("помощник меняет список админов: ждали ErrOwnerOnly, получили %v", err)
	}
	e.process(t)
	if alerts(e.env, "admin.denied") != 3 {
		t.Fatalf("владелец не получил тревоги о попытках: %+v", e.notes.ToAdmins)
	}
	denied := 0
	for _, a := range e.st.Audit() {
		if strings.HasPrefix(a.Action, "denied:") && a.AdminTg == helper {
			denied++
		}
	}
	if denied != 3 {
		t.Fatalf("попытки не записаны в журнал действий: %+v", e.st.Audit())
	}
}

func TestEveryAdminActionChecksPerms(t *testing.T) {
	e := newSecEnv(t)
	ctx := context.Background()
	e.addHelper(t, core.PermSupport)
	if _, err := e.c.SavePromo(ctx, helper, core.Promo{Code: "FREE100K", Kind: core.PromoMoney, Value: 10_000_000, Active: true}); !errors.Is(err, core.ErrForbidden) {
		t.Fatalf("помощник с поддержкой создал промокод: %v", err)
	}
	if _, err := e.c.SaveTariff(ctx, helper, core.Tariff{Name: "Даром", Active: true, Periods: []core.Period{{Days: 365, Price: 0}}}); !errors.Is(err, core.ErrForbidden) {
		t.Fatalf("помощник с поддержкой создал тариф: %v", err)
	}
	if _, err := e.c.AllTariffs(ctx, helper); !errors.Is(err, core.ErrForbidden) {
		t.Fatalf("помощник с поддержкой видит скрытые тарифы: %v", err)
	}
	e.addHelperTg(t, 3, core.PermPromo)
	if _, err := e.c.SavePromo(ctx, 3, core.Promo{Code: "SPRING26", Kind: core.PromoDays, Value: 7, Active: true}); err != nil {
		t.Fatal(err)
	}
	actions := map[string]int{}
	for _, a := range e.st.Audit() {
		actions[a.Action]++
	}
	if actions["denied:promo.save"] != 1 || actions["denied:tariff.save"] != 1 || actions["denied:tariff.list"] != 1 || actions["promo.save"] != 1 {
		t.Fatalf("журнал действий неполный: %v", actions)
	}
}

func TestHelperMoneyAlertsOwner(t *testing.T) {
	e := newSecEnv(t)
	ctx := context.Background()
	e.addHelper(t, core.PermGrant)
	if _, err := e.c.AdminAdjust(ctx, owner, e.user.ID, 500, "владелец"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.c.AdminAdjust(ctx, helper, e.user.ID, 700, "помощник"); err != nil {
		t.Fatal(err)
	}
	e.process(t)
	if n := alerts(e.env, "admin.helper_money"); n != 1 {
		t.Fatalf("тревог о деньгах от помощника %d, ждали 1 (владелец не шумит)", n)
	}
}

func TestLoginTwoSteps(t *testing.T) {
	e := newSecEnv(t)
	ctx := context.Background()
	st, err := e.c.LoginStart(ctx, owner, ownerPw, core.LoginInfo{IP: info, Agent: "Firefox"})
	if err != nil {
		t.Fatal(err)
	}
	id, nonce := st.ConfirmID, st.Nonce
	cf := e.lastConfirm(t)
	if cf.ConfirmID != id || cf.AdminTgID != owner || cf.Vars["ip"] != info || cf.Vars["agent"] != "Firefox" {
		t.Fatalf("сообщение о входе не то: %+v", cf)
	}
	if _, _, err := e.c.LoginFinish(ctx, id, nonce, core.LoginInfo{}); !errors.Is(err, core.ErrNotConfirmed) {
		t.Fatalf("без кнопки вход прошёл: %v", err)
	}
	if err := e.c.AnswerLogin(ctx, id, helper, st.Code); !errors.Is(err, core.ErrForbidden) {
		t.Fatalf("кнопку нажал чужой: ждали ErrForbidden, получили %v", err)
	}
	if err := e.c.AnswerConfirm(ctx, id, owner, true); !errors.Is(err, core.ErrInvalid) {
		t.Fatalf("вход подтверждён простым «Да» без кода: %v", err)
	}
	if err := e.c.AnswerLogin(ctx, id, owner, st.Code); err != nil {
		t.Fatal(err)
	}
	if err := e.c.AnswerLogin(ctx, id, owner, st.Code); !errors.Is(err, core.ErrConfirmGone) {
		t.Fatalf("повторное нажатие: ждали ErrConfirmGone, получили %v", err)
	}
	if _, _, err := e.c.LoginFinish(ctx, id, "чужой-ключ", core.LoginInfo{}); !errors.Is(err, core.ErrNotConfirmed) {
		t.Fatalf("вход с чужим ключом браузера: %v", err)
	}
	token, _, err := e.c.LoginFinish(ctx, id, nonce, core.LoginInfo{IP: info})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := e.c.LoginFinish(ctx, id, nonce, core.LoginInfo{IP: info}); err == nil {
		t.Fatal("одно подтверждение дало вторую сессию")
	}
	a, _, err := e.c.SessionAdmin(ctx, token, core.LoginInfo{})
	if err != nil || !a.Owner {
		t.Fatalf("сессия не работает: %+v %v", a, err)
	}
	e.process(t)
	if alerts(e.env, "admin.login") != 1 {
		t.Fatal("владелец не узнал о входе")
	}
}

func TestLoginConfirmExpires(t *testing.T) {
	e := newSecEnv(t)
	ctx := context.Background()
	st, err := e.c.LoginStart(ctx, owner, ownerPw, core.LoginInfo{IP: info})
	if err != nil {
		t.Fatal(err)
	}
	e.clock.Add(2*time.Minute + time.Second)
	if err := e.c.AnswerLogin(ctx, st.ConfirmID, owner, st.Code); !errors.Is(err, core.ErrConfirmGone) {
		t.Fatalf("кнопка после 2 минут: ждали ErrConfirmGone, получили %v", err)
	}
}

func TestLoginRejected(t *testing.T) {
	e := newSecEnv(t)
	ctx := context.Background()
	st, err := e.c.LoginStart(ctx, owner, ownerPw, core.LoginInfo{IP: info, Agent: "Chrome"})
	if err != nil {
		t.Fatal(err)
	}
	id, nonce := st.ConfirmID, st.Nonce
	if err := e.c.AnswerConfirm(ctx, id, owner, false); err != nil {
		t.Fatal(err)
	}
	if _, _, err := e.c.LoginFinish(ctx, id, nonce, core.LoginInfo{}); err == nil {
		t.Fatal("вход после «Нет» прошёл")
	}
	e.process(t)
	for _, a := range e.notes.ToAdmins {
		if a.Key == "admin.confirm_rejected" && a.Vars["ip"] == info && a.Vars["agent"] == "Chrome" {
			return
		}
	}
	t.Fatalf("тревога с IP и браузером не ушла: %+v", e.notes.ToAdmins)
}

func TestLoginBruteForce(t *testing.T) {
	e := newSecEnv(t)
	ctx := context.Background()
	for i := range 5 {
		if _, err := e.c.LoginStart(ctx, owner, "wrong-password", core.LoginInfo{IP: info}); !errors.Is(err, core.ErrBadLogin) {
			t.Fatalf("попытка %d: ждали ErrBadLogin, получили %v", i+1, err)
		}
	}
	if _, err := e.c.LoginStart(ctx, owner, ownerPw, core.LoginInfo{IP: info}); !errors.Is(err, core.ErrLoginLocked) {
		t.Fatalf("шестая попытка, даже с верным паролем: ждали ErrLoginLocked, получили %v", err)
	}
	e.process(t)
	if alerts(e.env, "admin.login_bruteforce") != 1 {
		t.Fatalf("владелец не получил уведомление о переборе: %+v", e.notes.ToAdmins)
	}
	e.clock.Add(time.Minute + time.Second)
	if _, err := e.c.LoginStart(ctx, owner, ownerPw, core.LoginInfo{IP: "5.6.7.8"}); err != nil {
		t.Fatalf("после паузы верный пароль должен проходить: %v", err)
	}
}

func TestUnknownAdminSameError(t *testing.T) {
	e := newSecEnv(t)
	if _, err := e.c.LoginStart(context.Background(), 999, ownerPw, core.LoginInfo{IP: info}); !errors.Is(err, core.ErrBadLogin) {
		t.Fatalf("незнакомый админ должен получать ту же ошибку, что неверный пароль: %v", err)
	}
}

func TestOwnerPassword(t *testing.T) {
	e := newEnv(t)
	c := core.New(core.Deps{Store: e.st, Clock: e.clock, Config: e.cfg, OwnerID: owner})
	if _, err := c.LoginStart(context.Background(), owner, "anything-long", core.LoginInfo{IP: info}); !errors.Is(err, core.ErrNoPassword) {
		t.Fatalf("первый запуск без пароля: ждали ErrNoPassword, получили %v", err)
	}
	if err := c.SetOwnerPassword(context.Background(), "short"); !errors.Is(err, core.ErrWeakPassword) {
		t.Fatalf("короткий пароль: ждали ErrWeakPassword, получили %v", err)
	}
}

func TestSessionsCloseAndExpire(t *testing.T) {
	e := newSecEnv(t)
	ctx := context.Background()
	e.addHelper(t, core.PermStats)
	helperToken := e.login(t, helper, helpPw)
	list, _ := e.c.Sessions(ctx, owner)
	if len(list) != 1 || list[0].AdminTg != helper {
		t.Fatalf("владелец не видит сессию помощника: %+v", list)
	}
	if mine, _ := e.c.Sessions(ctx, helper); len(mine) != 1 {
		t.Fatal("помощник не видит свою сессию")
	}
	if err := e.c.CloseSession(ctx, owner, list[0].ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := e.c.SessionAdmin(ctx, helperToken, core.LoginInfo{}); !errors.Is(err, core.ErrSessionGone) {
		t.Fatalf("закрытая сессия работает: %v", err)
	}
	token := e.login(t, owner, ownerPw)
	e.clock.Add(12*time.Hour + time.Second)
	if _, _, err := e.c.SessionAdmin(ctx, token, core.LoginInfo{}); !errors.Is(err, core.ErrSessionGone) {
		t.Fatalf("сессия старше 12 часов работает: %v", err)
	}
}

func TestDangerousNeedsOwnerYes(t *testing.T) {
	e := newSecEnv(t)
	ctx := context.Background()
	id, err := e.c.RequestSecret(ctx, owner, "yookassa.secret_key", "live_new_key")
	if err != nil {
		t.Fatal(err)
	}
	if e.cfg.Secret("yookassa.secret_key") != "" {
		t.Fatal("ключ сменился до «Да» владельца")
	}
	e.approve(t, id)
	if got := e.cfg.Secret("yookassa.secret_key"); got != "live_new_key" {
		t.Fatalf("после «Да» ключ не сменился: %q", got)
	}
	if _, err := e.c.RequestSecret(ctx, owner, "trial.days", "5"); !errors.Is(err, core.ErrInvalid) {
		t.Fatalf("обычная настройка не опасное действие: ждали ErrInvalid, получили %v", err)
	}
	e.process(t)
	if alerts(e.env, "admin.changed") != 1 {
		t.Fatal("владелец не узнал о смене ключа")
	}
}

func TestHelperRemovedLosesSessions(t *testing.T) {
	e := newSecEnv(t)
	ctx := context.Background()
	e.addHelper(t, core.PermGrant)
	token := e.login(t, helper, helpPw)
	id, err := e.c.RequestRemoveHelper(ctx, owner, helper)
	if err != nil {
		t.Fatal(err)
	}
	e.approve(t, id)
	if _, _, err := e.c.SessionAdmin(ctx, token, core.LoginInfo{}); err == nil {
		t.Fatal("отключённый помощник остался в админке")
	}
	if _, err := e.c.AdminAdjust(ctx, helper, e.user.ID, 100, "x"); !errors.Is(err, core.ErrForbidden) {
		t.Fatalf("отключённый помощник выдаёт деньги: %v", err)
	}
}

func TestAuditAndAlertsCannotBeOff(t *testing.T) {
	e := newSecEnv(t)
	e.addHelper(t, core.PermGrant)
	if _, err := e.c.AdminAdjust(core.WithIP(context.Background(), info), helper, e.user.ID, 3000, "компенсация"); err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, a := range e.st.Audit() {
		if a.Action == "balance.adjust" && a.AdminTg == helper && a.IP == info && a.Details["amount"] == "3000" {
			found = true
		}
	}
	if !found {
		t.Fatalf("действие помощника не в журнале: %+v", e.st.Audit())
	}
	for _, d := range settings.Registry() {
		if strings.Contains(d.Key, "alert") {
			t.Fatalf("тревогу можно выключить настройкой %s", d.Key)
		}
	}
	if err := e.cfg.Set("alerts.login", "false"); !errors.Is(err, settings.ErrUnknownKey) {
		t.Fatalf("попытка выключить тревогу: ждали ErrUnknownKey, получили %v", err)
	}
}

func TestForeignPayment(t *testing.T) {
	e := newPayEnv(t)
	ctx := context.Background()
	p, inv, err := e.c.CreatePayment(ctx, e.user.ID, "fake", 1000)
	if err != nil {
		t.Fatal(err)
	}
	thief, _ := e.c.Register(ctx, 4004, "thief", "ru")
	if err := e.c.CheckPayment(ctx, thief.ID, "fake", p.ID, inv.ExtAmount); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("чужой платёж в проверке: ждали ErrNotFound, получили %v", err)
	}
	ev := core.GatewayEvent{PaymentID: p.ID, ExternalID: p.ExternalID, ExtAmount: inv.ExtAmount, Status: core.PayPaid}
	if err := e.c.CreditOwn(ctx, thief.ID, "fake", ev); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("оплата чужого платежа: ждали ErrNotFound, получили %v", err)
	}
	if b := balance(t, e.st, thief.ID) + balance(t, e.st, e.user.ID); b != 0 {
		t.Fatalf("чужое нажатие зачислило деньги: %d", b)
	}
}

func TestBotToken(t *testing.T) {
	e := newSecEnv(t)
	ctx := context.Background()
	if _, err := e.c.RequestBotToken(ctx, owner, "123:bad"); !errors.Is(err, core.ErrBadToken) {
		t.Fatalf("неверный токен: ждали ErrBadToken, получили %v", err)
	}
	if e.cfg.Secret("bot.token") != "" || e.tg.webhook != "" {
		t.Fatal("неверный токен сохранён")
	}
	id, err := e.c.RequestBotToken(ctx, owner, "123:good")
	if err != nil {
		t.Fatal(err)
	}
	e.approve(t, id)
	if e.cfg.Secret("bot.token") != "123:good" || e.tg.webhook != "123:good" {
		t.Fatalf("токен не сменён или вебхук не переставлен: %q %q", e.cfg.Secret("bot.token"), e.tg.webhook)
	}
}

func TestAdminChatBinding(t *testing.T) {
	e := newSecEnv(t)
	ctx := context.Background()
	e.addHelper(t, core.PermSupport)
	if _, err := e.c.NewChatCode(ctx, helper); !errors.Is(err, core.ErrOwnerOnly) {
		t.Fatalf("код привязки помощнику: %v", err)
	}
	code, err := e.c.NewChatCode(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.c.BindChat(ctx, -100500, owner, "не-тот-код"); !errors.Is(err, core.ErrForbidden) {
		t.Fatalf("бота добавили в группу без кода: %v", err)
	}
	if err := e.c.BindChat(ctx, -100500, helper, code); !errors.Is(err, core.ErrForbidden) {
		t.Fatalf("привязывает не владелец: %v", err)
	}
	if _, err := e.c.AdminChat(ctx); !errors.Is(err, core.ErrNotFound) {
		t.Fatal("группа привязана без верного кода")
	}
	if err := e.c.BindChat(ctx, -100500, owner, code); err != nil {
		t.Fatal(err)
	}
	if ch, err := e.c.AdminChat(ctx); err != nil || ch.ChatID != -100500 {
		t.Fatalf("группа не привязана: %+v %v", ch, err)
	}
	if err := e.c.BindChat(ctx, -999, owner, code); !errors.Is(err, core.ErrForbidden) {
		t.Fatalf("код сработал второй раз: %v", err)
	}
	if !e.c.IsAdmin(ctx, helper, core.PermSupport) || e.c.IsAdmin(ctx, 777, core.PermSupport) {
		t.Fatal("проверка админов в группе не та")
	}
}

func TestStrangerCannotLockOwner(t *testing.T) {
	e := newSecEnv(t)
	ctx := context.Background()
	for range 20 {
		_, _ = e.c.LoginStart(ctx, owner, "guess-guess", core.LoginInfo{IP: "6.6.6.6"})
	}
	if _, err := e.c.LoginStart(ctx, owner, ownerPw, core.LoginInfo{IP: info}); err != nil {
		t.Fatalf("перебор с чужого IP заблокировал вход владельцу: %v", err)
	}
}

func TestParallelGuessesCounted(t *testing.T) {
	e := newSecEnv(t)
	ctx := context.Background()
	var wg sync.WaitGroup
	var passed atomic.Int32
	for range 30 {
		wg.Go(func() {
			if _, err := e.c.LoginStart(ctx, owner, "wrong-password", core.LoginInfo{IP: info}); !errors.Is(err, core.ErrLoginLocked) {
				passed.Add(1)
			}
		})
	}
	wg.Wait()
	if n := passed.Load(); n > 5 {
		t.Fatalf("пачка параллельных запросов проверила пароль %d раз, предел 5", n)
	}
}

func TestLoginInfoCleaned(t *testing.T) {
	e := newSecEnv(t)
	agent := "Safari\n\x1b[31mВаш iPhone, Москва" + strings.Repeat("x", 500)
	if _, err := e.c.LoginStart(context.Background(), owner, ownerPw, core.LoginInfo{IP: info, Agent: agent}); err != nil {
		t.Fatal(err)
	}
	cf := e.lastConfirm(t)
	if strings.ContainsAny(cf.Vars["agent"], "\n\x1b") || len([]rune(cf.Vars["agent"])) > 200 {
		t.Fatalf("браузер не очищен: %q", cf.Vars["agent"])
	}
}

func TestLoginWrongCodeBlocksAddress(t *testing.T) {
	e := newSecEnv(t)
	ctx := context.Background()
	st, err := e.c.LoginStart(ctx, owner, ownerPw, core.LoginInfo{IP: "7.7.7.7", Agent: "Firefox"})
	if err != nil {
		t.Fatal(err)
	}
	wrong := "10"
	if st.Code == wrong {
		wrong = "11"
	}
	if err := e.c.AnswerLogin(ctx, st.ConfirmID, owner, wrong); err != nil {
		t.Fatal(err)
	}
	if _, _, err := e.c.LoginFinish(ctx, st.ConfirmID, st.Nonce, core.LoginInfo{IP: "7.7.7.7"}); err == nil {
		t.Fatal("вход прошёл после неверного кода")
	}
	if _, err := e.c.LoginStart(ctx, owner, ownerPw, core.LoginInfo{IP: "7.7.7.7"}); !errors.Is(err, core.ErrLoginLocked) {
		t.Fatalf("после «не я» адрес должен быть закрыт даже с верным паролем: %v", err)
	}
	if _, err := e.c.LoginStart(ctx, owner, ownerPw, core.LoginInfo{IP: info}); err != nil {
		t.Fatalf("свой адрес закрываться не должен: %v", err)
	}
	e.process(t)
	if alerts(e.env, "admin.confirm_rejected") != 1 {
		t.Fatalf("тревога о неверном коде не ушла: %+v", e.notes.ToAdmins)
	}
}

func TestLoginCodesOffered(t *testing.T) {
	e := newSecEnv(t)
	st, err := e.c.LoginStart(context.Background(), owner, ownerPw, core.LoginInfo{IP: info})
	if err != nil {
		t.Fatal(err)
	}
	codes := strings.Split(e.lastConfirm(t).Vars["codes"], ",")
	seen := map[string]bool{}
	for _, c := range codes {
		seen[c] = true
	}
	if len(codes) != 3 || len(seen) != 3 || !seen[st.Code] {
		t.Fatalf("кнопки %v, код браузера %s", codes, st.Code)
	}
}

func TestLoginSpreadAlarm(t *testing.T) {
	e := newSecEnv(t)
	ctx := context.Background()
	for i := range 25 {
		ip := "9.9." + itoa(i) + ".1"
		if _, err := e.c.LoginStart(ctx, owner, "wrong-password", core.LoginInfo{IP: ip}); !errors.Is(err, core.ErrBadLogin) {
			t.Fatalf("попытка с %s: %v", ip, err)
		}
	}
	e.process(t)
	if alerts(e.env, "admin.login_spread") != 1 {
		t.Fatalf("перебор с разных адресов без тревоги: %+v", e.notes.ToAdmins)
	}
	if _, err := e.c.LoginStart(ctx, owner, ownerPw, core.LoginInfo{IP: info}); err != nil {
		t.Fatalf("владелец со своего адреса должен входить: %v", err)
	}
}

func TestLoginMessagesLimited(t *testing.T) {
	e := newSecEnv(t)
	ctx := context.Background()
	for range 5 {
		if _, err := e.c.LoginStart(ctx, owner, ownerPw, core.LoginInfo{IP: info}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := e.c.LoginStart(ctx, owner, ownerPw, core.LoginInfo{IP: info}); !errors.Is(err, core.ErrTooManyTries) {
		t.Fatalf("шестое сообщение о входе за 10 минут: %v", err)
	}
}

func TestLoginNeedsAddress(t *testing.T) {
	e := newSecEnv(t)
	if _, err := e.c.LoginStart(context.Background(), owner, ownerPw, core.LoginInfo{}); !errors.Is(err, core.ErrInvalid) {
		t.Fatalf("вход без IP: %v", err)
	}
}

func TestSessionIdleAndNewAddress(t *testing.T) {
	e := newSecEnv(t)
	ctx := context.Background()
	token := e.login(t, owner, ownerPw)
	e.clock.Add(50 * time.Minute)
	if _, _, err := e.c.SessionAdmin(ctx, token, core.LoginInfo{IP: info}); err != nil {
		t.Fatalf("сессия через 50 минут: %v", err)
	}
	for range 2 {
		if _, _, err := e.c.SessionAdmin(ctx, token, core.LoginInfo{IP: "8.8.8.8"}); err != nil {
			t.Fatalf("смена адреса не закрывает сессию: %v", err)
		}
	}
	e.process(t)
	if alerts(e.env, "admin.session_ip") != 1 {
		t.Fatalf("тревог о новом адресе сессии не одна: %+v", e.notes.ToAdmins)
	}
	e.clock.Add(61 * time.Minute)
	if _, _, err := e.c.SessionAdmin(ctx, token, core.LoginInfo{IP: info}); !errors.Is(err, core.ErrSessionGone) {
		t.Fatalf("сессия после часа простоя: %v", err)
	}
}

func TestHelperNeedsPermsAndHashHidden(t *testing.T) {
	e := newSecEnv(t)
	if _, err := e.c.RequestAddHelper(context.Background(), owner, 5, nil, helpPw); !errors.Is(err, core.ErrInvalid) {
		t.Fatalf("помощник без прав: %v", err)
	}
	e.addHelper(t, core.PermStats)
	list, err := e.c.Admins(context.Background(), owner)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(list)
	if strings.Contains(string(b), "argon2") {
		t.Fatalf("хэш пароля в списке админов: %s", b)
	}
}

func itoa(n int) string { return strconv.Itoa(n) }

func TestOldOwnerCannotLogin(t *testing.T) {
	e := newSecEnv(t)
	// Владельца сменили в .env: прежний со своим паролем больше не входит.
	c := core.New(core.Deps{Store: e.st, Clock: e.clock, Config: e.cfg, OwnerID: 77})
	if _, err := c.LoginStart(context.Background(), owner, ownerPw, core.LoginInfo{IP: info}); !errors.Is(err, core.ErrBadLogin) {
		t.Fatalf("прежний владелец: %v", err)
	}
}

func TestExternalActionRechecksAuthor(t *testing.T) {
	e := newSecEnv(t)
	ctx := context.Background()
	var sent []string
	e.c.OnApproved("broadcast", core.PermBroadcast, func(_ context.Context, payload []byte) error {
		var text string
		if err := json.Unmarshal(payload, &text); err != nil {
			return err
		}
		sent = append(sent, text)
		return nil
	})
	e.addHelper(t, core.PermBroadcast)
	if _, err := e.c.RequestAction(ctx, helper, "broadcast", strings.Repeat("я", 3001)); !errors.Is(err, core.ErrTooLong) {
		t.Fatalf("текст длиннее сообщения владельцу: %v", err)
	}
	text := "Скидка " + strings.Repeat("!", 400) + " https://phish.example"
	id, err := e.c.RequestAction(ctx, helper, "broadcast", text)
	if err != nil {
		t.Fatal(err)
	}
	if got := e.lastConfirm(t).Vars["details"]; !strings.Contains(got, "https://phish.example") {
		t.Fatalf("владелец видит не весь текст: %q", got)
	}
	e.approve(t, id)
	if len(sent) != 1 || sent[0] != text {
		t.Fatalf("после «Да» ушло не то: %q", sent)
	}

	// Помощника отключили, пока владелец не нажал «Да»: действие не выполняется.
	id, err = e.c.RequestAction(ctx, helper, "broadcast", "ещё")
	if err != nil {
		t.Fatal(err)
	}
	rm, err := e.c.RequestRemoveHelper(ctx, owner, helper)
	if err != nil {
		t.Fatal(err)
	}
	e.approve(t, rm)
	if err := e.c.AnswerConfirm(ctx, id, owner, true); !errors.Is(err, core.ErrForbidden) {
		t.Fatalf("действие отключённого помощника: %v", err)
	}
	if len(sent) != 1 {
		t.Fatalf("действие отключённого помощника выполнено: %q", sent)
	}
}

func TestExternalActionNeedsItsPerm(t *testing.T) {
	e := newSecEnv(t)
	e.c.OnApproved("export", core.PermStats, func(context.Context, []byte) error { return nil })
	e.addHelper(t, core.PermBroadcast)
	if _, err := e.c.RequestAction(context.Background(), helper, "export", nil); !errors.Is(err, core.ErrForbidden) {
		t.Fatalf("выгрузка без права: %v", err)
	}
}

func TestSecretTailNotInAudit(t *testing.T) {
	e := newSecEnv(t)
	if _, err := e.c.RequestSecret(context.Background(), owner, "yookassa.secret_key", "live_0123456789abcdef"); err != nil {
		t.Fatal(err)
	}
	if e.lastConfirm(t).Vars["tail"] != "cdef" {
		t.Fatal("владелец не видит хвост ключа")
	}
	for _, a := range e.st.Audit() {
		if _, ok := a.Details["tail"]; ok {
			t.Fatalf("хвост ключа в журнале действий: %+v", a)
		}
	}
}
