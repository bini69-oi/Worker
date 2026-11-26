package bot

import (
	"context"
	"strings"
	"testing"

	"github.com/bini69-oi/worker/core"
)

func TestConnectScreen(t *testing.T) {
	e := newEnv(t)
	u := e.start(t)
	tr := e.tariff(t, "Месяц", core.Period{Days: 30, Price: 100})
	e.topup(t, u.ID, 1000)
	if _, err := e.c.Buy(context.Background(), u.ID, tr.ID, 30); err != nil {
		t.Fatal(err)
	}
	e.press(t, userTg, 5, "con")
	if got := render(e.api.take()); !strings.Contains(got, "оформляется") {
		t.Fatalf("до панели ссылки нет, а экран не сказал: %s", got)
	}
	e.sync(t)
	e.press(t, userTg, 5, "con")
	calls := e.api.take()
	got := render(calls)
	// Ни одной своей схемы текстом, ни ссылки подписки и id в адресе перехода.
	text := calls[0].text
	if strings.Contains(text, "://") && strings.Contains(text, "happ") {
		t.Fatalf("в тексте экрана своя схема: %s", text)
	}
	for _, row := range calls[0].markup.InlineKeyboard {
		for _, btn := range row {
			if strings.Contains(btn.URL, "/dl/") && (strings.Contains(btn.URL, "sub.test") || strings.Contains(btn.URL, "/1001")) {
				t.Fatalf("в адресе перехода ссылка подписки или id: %s", btn.URL)
			}
		}
	}
	if strings.Count(got, "Добавить в") != 2 {
		t.Fatalf("на экране не два приложения: %s", got)
	}
}

func TestConnectAppsSetting(t *testing.T) {
	e := newEnv(t)
	if err := e.cfg.Set("connect.apps", "happ,nope"); err == nil {
		t.Fatal("неизвестное приложение сохранилось")
	}
	if err := e.cfg.Set("connect.apps", "hiddify"); err != nil {
		t.Fatal(err)
	}
}
