package api

import "testing"

func TestLogout(t *testing.T) {
	e := newAppEnv(t)
	a, b := e.login(9), e.login(9)
	if rec := e.as(a, "POST", "/api/app/logout", "{}"); rec.Code != 200 {
		t.Fatalf("выход: %d %s", rec.Code, rec.Body)
	}
	if rec := e.as(a, "POST", "/api/app/logout", "{}"); rec.Code != 401 {
		t.Fatalf("закрытая сессия: %d", rec.Code)
	}
	if rec := e.as(b, "POST", "/api/app/logout-all", "{}"); rec.Code != 200 {
		t.Fatalf("соседняя сессия закрылась вместе с первой: %d", rec.Code)
	}
}

func TestLogoutAll(t *testing.T) {
	e := newAppEnv(t)
	tokens := []string{e.login(9), e.login(9), e.login(9)}
	other := e.login(10)
	if rec := e.as(tokens[0], "POST", "/api/app/logout-all", "{}"); rec.Code != 200 {
		t.Fatalf("выйти везде: %d", rec.Code)
	}
	for i, tok := range tokens {
		if rec := e.as(tok, "POST", "/api/app/logout", "{}"); rec.Code != 401 {
			t.Errorf("сессия %d после «выйти везде»: %d", i, rec.Code)
		}
	}
	if rec := e.as(other, "POST", "/api/app/logout", "{}"); rec.Code != 200 {
		t.Fatalf("сессия другого абонента закрылась: %d", rec.Code)
	}
}

func TestBanClosesSessions(t *testing.T) {
	e := newAppEnv(t)
	tok := e.login(9)
	u, _ := e.c.UserByTg(t.Context(), 9)
	if err := e.c.Ban(t.Context(), testOwner, u.ID, "спам"); err != nil {
		t.Fatal(err)
	}
	if rec := e.as(tok, "POST", "/api/app/logout", "{}"); rec.Code != 403 {
		t.Fatalf("сессия забаненного: %d", rec.Code)
	}
	// После разбана видно, что сессия закрыта.
	if err := e.c.Unban(t.Context(), testOwner, u.ID, ""); err != nil {
		t.Fatal(err)
	}
	if rec := e.as(tok, "POST", "/api/app/logout", "{}"); rec.Code != 401 {
		t.Fatalf("сессия после разбана: %d", rec.Code)
	}
}

func TestNoToken(t *testing.T) {
	e := newAppEnv(t)
	for _, tok := range []string{"", "x", "не токен"} {
		if rec := e.as(tok, "POST", "/api/app/logout", "{}"); rec.Code != 401 {
			t.Errorf("токен %q: %d", tok, rec.Code)
		}
	}
	// Перебор токенов с одного адреса упирается в предел.
	limited := false
	for i := range 80 {
		e.clock.Add(200_000_000) // 0.2 с: общий предел запросов в секунду не мешает
		if e.as("tok"+string(rune('a'+i%20)), "POST", "/api/app/logout", "{}").Code == 429 {
			limited = true
		}
	}
	if !limited {
		t.Fatal("перебор токенов не ограничен")
	}
}
