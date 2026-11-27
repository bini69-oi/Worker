package core_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/bini69-oi/worker/core"
)

var webInfo = core.LoginInfo{IP: "5.6.7.8", Agent: "Safari"}

func TestUserSessionTelegramLifetime(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	tok, s, err := e.c.OpenSession(ctx, e.user.ID, core.SessionTg, webInfo)
	if err != nil || tok == "" {
		t.Fatalf("сессия не создана: %v", err)
	}
	if !s.ExpiresAt.Equal(t0.Add(24 * time.Hour)) {
		t.Fatalf("сессия Telegram живёт до %v", s.ExpiresAt)
	}
	u, _, err := e.c.SessionUser(ctx, tok)
	if err != nil || u.ID != e.user.ID {
		t.Fatalf("свежая сессия: %v", err)
	}
	// Без движения сколько угодно, но не дольше суток от выдачи.
	e.clock.Add(23 * time.Hour)
	if _, _, err := e.c.SessionUser(ctx, tok); err != nil {
		t.Fatalf("сессия Telegram умерла раньше срока: %v", err)
	}
	e.clock.Add(2 * time.Hour)
	if _, _, err := e.c.SessionUser(ctx, tok); !errors.Is(err, core.ErrSessionGone) {
		t.Fatalf("старая сессия Telegram: %v", err)
	}
}

func TestUserSessionBrowserLifetime(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	tok, s, err := e.c.OpenSession(ctx, e.user.ID, core.SessionWeb, webInfo)
	if err != nil {
		t.Fatal(err)
	}
	if !s.ExpiresAt.Equal(t0.Add(30 * 24 * time.Hour)) {
		t.Fatalf("сессия браузера живёт до %v", s.ExpiresAt)
	}
	// Каждые шесть дней заходит: живёт.
	for range 3 {
		e.clock.Add(6 * 24 * time.Hour)
		if _, _, err := e.c.SessionUser(ctx, tok); err != nil {
			t.Fatalf("активная сессия умерла: %v", err)
		}
	}
	// Неделя без запросов: закрыта.
	e.clock.Add(8 * 24 * time.Hour)
	if _, _, err := e.c.SessionUser(ctx, tok); !errors.Is(err, core.ErrSessionGone) {
		t.Fatalf("8 дней без запросов: %v", err)
	}
	// И выше срока 30 дней при любой активности.
	tok2, _, _ := e.c.OpenSession(ctx, e.user.ID, core.SessionWeb, webInfo)
	for range 5 {
		e.clock.Add(6 * 24 * time.Hour)
		_, _, err = e.c.SessionUser(ctx, tok2)
	}
	if !errors.Is(err, core.ErrSessionGone) {
		t.Fatalf("сессия старше 30 дней: %v", err)
	}
}

func TestUserSessionClose(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	a, _, _ := e.c.OpenSession(ctx, e.user.ID, core.SessionTg, webInfo)
	b, _, _ := e.c.OpenSession(ctx, e.user.ID, core.SessionWeb, webInfo)
	if err := e.c.Logout(ctx, a); err != nil {
		t.Fatal(err)
	}
	if _, _, err := e.c.SessionUser(ctx, a); !errors.Is(err, core.ErrSessionGone) {
		t.Fatalf("закрытая сессия: %v", err)
	}
	if _, _, err := e.c.SessionUser(ctx, b); err != nil {
		t.Fatalf("соседняя сессия закрылась: %v", err)
	}
	c2, _, _ := e.c.OpenSession(ctx, e.user.ID, core.SessionTg, webInfo)
	if err := e.c.LogoutAll(ctx, e.user.ID); err != nil {
		t.Fatal(err)
	}
	for _, tok := range []string{b, c2} {
		if _, _, err := e.c.SessionUser(ctx, tok); !errors.Is(err, core.ErrSessionGone) {
			t.Fatalf("после «выйти везде»: %v", err)
		}
	}
}

func TestUserSessionUnknownAndBan(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	if _, _, err := e.c.SessionUser(ctx, "нет такого"); !errors.Is(err, core.ErrSessionGone) {
		t.Fatalf("чужой токен: %v", err)
	}
	tok, _, _ := e.c.OpenSession(ctx, e.user.ID, core.SessionTg, webInfo)
	if err := e.c.Ban(ctx, owner, e.user.ID, ""); err != nil {
		t.Fatal(err)
	}
	if _, _, err := e.c.SessionUser(ctx, tok); !errors.Is(err, core.ErrSessionGone) {
		t.Fatalf("бан не закрыл сессию: %v", err)
	}
	if _, _, err := e.c.OpenSession(ctx, e.user.ID, core.SessionTg, webInfo); !errors.Is(err, core.ErrBanned) {
		t.Fatalf("забаненному выдана сессия: %v", err)
	}
}

func TestTgLogin(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	tok, u, err := e.c.TgLogin(ctx, 4242, "new", "en-US", "ref_"+e.user.RefCode, webInfo)
	if err != nil {
		t.Fatal(err)
	}
	if u.TgID != 4242 || u.Lang != "en" || u.ReferrerID != e.user.ID {
		t.Fatalf("новый абонент: %+v", u)
	}
	if got, _, err := e.c.SessionUser(ctx, tok); err != nil || got.ID != u.ID {
		t.Fatalf("сессия нового абонента: %v", err)
	}
	// Повторный вход находит того же абонента и язык не меняет.
	_, again, err := e.c.TgLogin(ctx, 4242, "new", "ru", "", webInfo)
	if err != nil || again.ID != u.ID || again.Lang != "en" {
		t.Fatalf("повторный вход: %+v %v", again, err)
	}
	// Мусор в start_param реферала не создаёт.
	_, junk, err := e.c.TgLogin(ctx, 4243, "", "ru", "ref_'; drop", webInfo)
	if err != nil || junk.ReferrerID != 0 {
		t.Fatalf("мусорный start_param: %+v %v", junk, err)
	}
	if err := e.c.Ban(ctx, owner, u.ID, ""); err != nil {
		t.Fatal(err)
	}
	if _, _, err := e.c.TgLogin(ctx, 4242, "new", "ru", "", webInfo); !errors.Is(err, core.ErrBanned) {
		t.Fatalf("забаненный вошёл: %v", err)
	}
}
