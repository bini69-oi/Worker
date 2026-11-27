package core_test

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/bini69-oi/worker/core"
)

// webLogin начинает вход в браузере и открывает ссылку в боте от имени абонента tgID.
func webLogin(t *testing.T, e *env, tgID int64) (core.WebLoginStarted, core.WebLoginPrompt) {
	t.Helper()
	ctx := context.Background()
	st, err := e.c.WebLoginStart(ctx, webInfo)
	if err != nil {
		t.Fatal(err)
	}
	p, err := e.c.WebLoginOpen(ctx, st.Nonce, tgID)
	if err != nil {
		t.Fatal(err)
	}
	return st, p
}

func TestWebLoginRightCode(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	st, p := webLogin(t, e, e.user.TgID)
	if len(st.Code) != 2 || len(p.Codes) != 3 || !slices.Contains(p.Codes, st.Code) {
		t.Fatalf("код сайта %q, коды бота %v", st.Code, p.Codes)
	}
	if p.IP != webInfo.IP || p.Device.Browser != "Safari" {
		t.Fatalf("откуда входят: %+v", p)
	}
	if res, err := e.c.WebLoginPoll(ctx, st.ID, webInfo); err != nil || res.State != core.LoginPending || res.Token != "" {
		t.Fatalf("до нажатия: %+v %v", res, err)
	}
	ans, err := e.c.WebLoginAnswer(ctx, p.ID, e.user.TgID, st.Code)
	if err != nil || !ans.Confirmed {
		t.Fatalf("верный код: %v %v", ans, err)
	}
	res, err := e.c.WebLoginPoll(ctx, st.ID, webInfo)
	if err != nil || res.State != core.LoginConfirmed || res.Token == "" {
		t.Fatalf("после нажатия: %+v %v", res, err)
	}
	if u, s, err := e.c.SessionUser(ctx, res.Token); err != nil || u.ID != e.user.ID || s.Kind != core.SessionWeb {
		t.Fatalf("сессия входа: %v", err)
	}
	// Сессия выдаётся один раз.
	if res, _ := e.c.WebLoginPoll(ctx, st.ID, webInfo); res.State != core.LoginExpired || res.Token != "" {
		t.Fatalf("второй опрос: %+v", res)
	}
	// Второе нажатие и повторное открытие ссылки ничего не меняют.
	if _, err := e.c.WebLoginAnswer(ctx, p.ID, e.user.TgID, st.Code); !errors.Is(err, core.ErrLoginUsed) {
		t.Fatalf("второе нажатие: %v", err)
	}
	if _, err := e.c.WebLoginOpen(ctx, st.Nonce, e.user.TgID); !errors.Is(err, core.ErrLoginUsed) {
		t.Fatalf("повторное открытие: %v", err)
	}
}

func TestWebLoginWrongCode(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	st, p := webLogin(t, e, e.user.TgID)
	wrong := ""
	for _, c := range p.Codes {
		if c != st.Code {
			wrong = c
		}
	}
	ans, err := e.c.WebLoginAnswer(ctx, p.ID, e.user.TgID, wrong)
	if err != nil || ans.Confirmed {
		t.Fatalf("неверный код: %v %v", ans, err)
	}
	if res, _ := e.c.WebLoginPoll(ctx, st.ID, webInfo); res.State != core.LoginRejected || res.Token != "" {
		t.Fatalf("после неверного кода: %+v", res)
	}
	// Запрос закрыт: верный код после этого уже не подходит.
	if _, err := e.c.WebLoginAnswer(ctx, p.ID, e.user.TgID, st.Code); !errors.Is(err, core.ErrLoginUsed) {
		t.Fatalf("верный код после отказа: %v", err)
	}
}

func TestWebLoginNotMe(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	st, p := webLogin(t, e, e.user.TgID)
	if a, err := e.c.WebLoginAnswer(ctx, p.ID, e.user.TgID, "no"); err != nil || a.Confirmed {
		t.Fatalf("«Это не я»: %v %v", a, err)
	}
	if res, _ := e.c.WebLoginPoll(ctx, st.ID, webInfo); res.State != core.LoginRejected || res.Token != "" {
		t.Fatalf("сессия после «Это не я»: %+v", res)
	}
}

func TestWebLoginExpired(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	st, p := webLogin(t, e, e.user.TgID)
	e.clock.Add(5*time.Minute + time.Second)
	if _, err := e.c.WebLoginAnswer(ctx, p.ID, e.user.TgID, st.Code); !errors.Is(err, core.ErrLoginExpired) {
		t.Fatalf("нажатие после срока: %v", err)
	}
	if _, err := e.c.WebLoginOpen(ctx, st.Nonce, e.user.TgID); !errors.Is(err, core.ErrLoginExpired) {
		t.Fatalf("ссылка после срока: %v", err)
	}
	if res, _ := e.c.WebLoginPoll(ctx, st.ID, webInfo); res.State != core.LoginExpired {
		t.Fatalf("опрос после срока: %+v", res)
	}
	if _, err := e.c.WebLoginOpen(ctx, "нет такой", e.user.TgID); !errors.Is(err, core.ErrLoginExpired) {
		t.Fatalf("неизвестная ссылка: %v", err)
	}
}

func TestWebLoginSecondAccount(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	other, _ := e.c.Register(ctx, 2002, "mallory", "ru")
	st, p := webLogin(t, e, e.user.TgID)
	// Ссылку открыл другой аккаунт: она уже занята, нажать чужое сообщение он тоже не может.
	if _, err := e.c.WebLoginOpen(ctx, st.Nonce, other.TgID); !errors.Is(err, core.ErrLoginUsed) {
		t.Fatalf("второй аккаунт открыл ссылку: %v", err)
	}
	if _, err := e.c.WebLoginAnswer(ctx, p.ID, other.TgID, st.Code); !errors.Is(err, core.ErrForbidden) {
		t.Fatalf("второй аккаунт нажал код: %v", err)
	}
	if a, err := e.c.WebLoginAnswer(ctx, p.ID, e.user.TgID, st.Code); err != nil || !a.Confirmed {
		t.Fatalf("первый аккаунт после этого: %v %v", a, err)
	}
}

func TestWebLoginBannedAndUnknownID(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	st, p := webLogin(t, e, e.user.TgID)
	if _, err := e.c.WebLoginAnswer(ctx, p.ID, e.user.TgID, st.Code); err != nil {
		t.Fatal(err)
	}
	if err := e.c.Ban(ctx, owner, e.user.ID, ""); err != nil {
		t.Fatal(err)
	}
	if res, err := e.c.WebLoginPoll(ctx, st.ID, webInfo); !errors.Is(err, core.ErrBanned) || res.Token != "" {
		t.Fatalf("забанили между нажатием и опросом: %+v %v", res, err)
	}
	if res, err := e.c.WebLoginPoll(ctx, "чужой id", webInfo); err != nil || res.State != core.LoginExpired {
		t.Fatalf("неизвестный id: %+v %v", res, err)
	}
}

func TestWebLoginCodesDiffer(t *testing.T) {
	e := newEnv(t)
	for range 30 {
		st, p := webLogin(t, e, e.user.TgID)
		seen := map[string]bool{}
		for _, c := range p.Codes {
			if len(c) != 2 || seen[c] {
				t.Fatalf("коды %v (код сайта %s)", p.Codes, st.Code)
			}
			seen[c] = true
		}
	}
}

var chromeWin = core.LoginInfo{IP: "5.6.7.8", Agent: "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/126.0 Safari/537.36"}

// Строка браузера клиента не доходит до бота как свободный текст: только распознанные слова.
func TestWebLoginPromptShowsOnlyRecognizedDevice(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	st, _ := e.c.WebLoginStart(ctx, core.LoginInfo{IP: "5.6.7.8", Agent: "ВНИМАНИЕ <b>нажмите 42</b> Chrome/1"})
	p, err := e.c.WebLoginOpen(ctx, st.Nonce, e.user.TgID)
	if err != nil {
		t.Fatal(err)
	}
	if d := p.Device.String(); d != "Chrome" {
		t.Fatalf("устройство %q", d)
	}
	st2, _ := e.c.WebLoginStart(ctx, chromeWin)
	p2, _ := e.c.WebLoginOpen(ctx, st2.Nonce, e.user.TgID)
	if p2.Device.String() != "Chrome, Windows" {
		t.Fatalf("устройство %q", p2.Device.String())
	}
}

func wrongCode(p core.WebLoginPrompt, right string) string {
	for _, c := range p.Codes {
		if c != right {
			return c
		}
	}
	return ""
}

// Три неверных нажатия за час закрывают вход в браузере для абонента на час; абонент узнаёт об этом.
func TestWebLoginThreeFailuresLock(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	var last core.LoginAnswer
	for i := 1; i <= 3; i++ {
		st, p := webLogin(t, e, e.user.TgID)
		a, err := e.c.WebLoginAnswer(ctx, p.ID, e.user.TgID, wrongCode(p, st.Code))
		if err != nil || a.Confirmed {
			t.Fatalf("попытка %d: %+v %v", i, a, err)
		}
		if a.Locked != (i == 3) {
			t.Fatalf("попытка %d: Locked=%v", i, a.Locked)
		}
		last = a
		e.clock.Add(time.Minute)
	}
	if !last.Locked {
		t.Fatal("третья неудача не закрыла вход")
	}
	// Вход закрыт: ссылка не открывается, даже новая.
	st, _ := e.c.WebLoginStart(ctx, webInfo)
	if _, err := e.c.WebLoginOpen(ctx, st.Nonce, e.user.TgID); !errors.Is(err, core.ErrLoginLocked) {
		t.Fatalf("открытие при блоке: %v", err)
	}
	// Через час снова можно, и верный код проходит.
	e.clock.Add(time.Hour)
	st, p := webLogin(t, e, e.user.TgID)
	if a, err := e.c.WebLoginAnswer(ctx, p.ID, e.user.TgID, st.Code); err != nil || !a.Confirmed {
		t.Fatalf("после часа: %+v %v", a, err)
	}
}

// Неверный код отменяет именно этот вход, и верный код после него уже не подходит.
func TestWebLoginFailureCancelsThisLogin(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	st, p := webLogin(t, e, e.user.TgID)
	if _, err := e.c.WebLoginAnswer(ctx, p.ID, e.user.TgID, wrongCode(p, st.Code)); err != nil {
		t.Fatal(err)
	}
	if _, err := e.c.WebLoginAnswer(ctx, p.ID, e.user.TgID, st.Code); !errors.Is(err, core.ErrLoginUsed) {
		t.Fatalf("верный код после неверного: %v", err)
	}
	if res, _ := e.c.WebLoginPoll(ctx, st.ID, webInfo); res.State != core.LoginRejected {
		t.Fatalf("сайт: %+v", res)
	}
}

// «Это не я» в счёт блокировки не идёт: так отвечает тот, кому прислали чужую ссылку, и вход ему не закрывается.
func TestWebLoginNotMeDoesNotLock(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	for range 5 {
		_, p := webLogin(t, e, e.user.TgID)
		if a, err := e.c.WebLoginAnswer(ctx, p.ID, e.user.TgID, "no"); err != nil || a.Locked || a.Confirmed {
			t.Fatalf("«Это не я»: %+v %v", a, err)
		}
	}
	st, p := webLogin(t, e, e.user.TgID)
	if a, err := e.c.WebLoginAnswer(ctx, p.ID, e.user.TgID, st.Code); err != nil || !a.Confirmed {
		t.Fatalf("после пяти «Это не я»: %+v %v", a, err)
	}
}

// Окно счёта неудач скользит: две неудачи и пауза больше часа не складываются в блок.
func TestWebLoginFailuresExpire(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	for range 2 {
		st, p := webLogin(t, e, e.user.TgID)
		_, _ = e.c.WebLoginAnswer(ctx, p.ID, e.user.TgID, wrongCode(p, st.Code))
	}
	e.clock.Add(61 * time.Minute)
	st, p := webLogin(t, e, e.user.TgID)
	a, err := e.c.WebLoginAnswer(ctx, p.ID, e.user.TgID, wrongCode(p, st.Code))
	if err != nil || a.Locked {
		t.Fatalf("третья неудача через час после первых двух: %+v %v", a, err)
	}
}
