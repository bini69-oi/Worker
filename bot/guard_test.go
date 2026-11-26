package bot

import (
	"context"
	"strings"
	"testing"
	"time"
)

// inputEnv ставит кнопку «ждать код» и записывает, что пришло в обработчик ввода.
func inputEnv(t *testing.T) (*env, *[]string) {
	e := newEnv(t)
	var got []string
	e.b.acts["askpromo"] = func(_ context.Context, r *req, _ Data) error { e.b.expect(r, waitPromo); return nil }
	e.b.inputs[waitPromo] = func(_ context.Context, _ *req, _ wait, text string) error { got = append(got, text); return nil }
	return e, &got
}

func TestInputWaited(t *testing.T) {
	e, got := inputEnv(t)
	e.press(t, userTg, 5, "askpromo")
	e.text(t, userTg, "SPRING26")
	if strings.Join(*got, ",") != "SPRING26" {
		t.Fatalf("ожидаемый текст не дошёл: %v", *got)
	}
	// Ожидание одноразовое: следующий текст уже в поддержку.
	e.text(t, userTg, "ещё текст")
	if len(*got) != 1 {
		t.Fatalf("второй текст тоже принят за код: %v", *got)
	}
}

func TestInputResetByButton(t *testing.T) {
	e, got := inputEnv(t)
	e.press(t, userTg, 5, "askpromo")
	e.press(t, userTg, 5, "m") // «Назад»
	e.api.take()
	e.text(t, userTg, "привет")
	if len(*got) != 0 {
		t.Fatalf("текст после «Назад» принят за код: %v", *got)
	}
	if c := e.api.take(); len(c) == 0 || !strings.Contains(c[len(c)-1].text, "поддержк") {
		t.Fatalf("текст не ушёл в поддержку: %s", render(c))
	}
}

func TestInputResetByCommandAndTime(t *testing.T) {
	e, got := inputEnv(t)
	e.press(t, userTg, 5, "askpromo")
	e.text(t, userTg, "/start")
	e.text(t, userTg, "SPRING26")
	if len(*got) != 0 {
		t.Fatalf("команда не сбросила ожидание: %v", *got)
	}
	e.press(t, userTg, 5, "askpromo")
	e.clock.Add(11 * time.Minute)
	e.text(t, userTg, "SPRING26")
	if len(*got) != 0 {
		t.Fatalf("ожидание пережило 10 минут: %v", *got)
	}
}

func TestFlood(t *testing.T) {
	e := newEnv(t)
	e.step = 0
	hits := 0
	e.b.acts["hit"] = func(context.Context, *req, Data) error { hits++; return nil }
	for range 20 {
		e.press(t, userTg, 5, "hit")
	}
	warns := 0
	for _, c := range e.api.take() {
		if c.method == "answer" && c.text == "Слишком часто, подождите секунду" {
			warns++
		}
	}
	if hits != 5 || warns != 1 {
		t.Fatalf("20 нажатий за секунду: обработано %d, предупреждений %d", hits, warns)
	}
	e.clock.Add(time.Second)
	e.press(t, userTg, 5, "hit")
	if hits != 6 {
		t.Fatal("после паузы нажатие не прошло")
	}
}

func TestBannedSeesOnlyNotice(t *testing.T) {
	e := newEnv(t)
	hits := 0
	e.b.acts["hit"] = func(context.Context, *req, Data) error { hits++; return nil }
	e.text(t, userTg, "/start")
	u, _ := e.c.UserByTg(context.Background(), userTg)
	if err := e.c.Ban(context.Background(), owner, u.ID, "тест"); err != nil {
		t.Fatal(err)
	}
	e.api.take()
	e.press(t, userTg, 5, "hit")
	e.press(t, userTg, 5, "hit")
	notices := 0
	for _, c := range e.api.take() {
		if c.method == "send" && strings.Contains(c.text, "заблокирован") {
			notices++
		}
	}
	if hits != 0 || notices != 1 {
		t.Fatalf("забаненный: обработано %d, текстов о бане %d", hits, notices)
	}
}

func TestMaintenanceGate(t *testing.T) {
	e := newEnv(t)
	hits := 0
	e.b.acts["hit"] = func(context.Context, *req, Data) error { hits++; return nil }
	e.set(t, "maintenance.on", "true")
	e.press(t, userTg, 5, "hit")
	calls := e.api.take()
	if hits != 0 || !strings.Contains(render(calls), "технические работы") {
		t.Fatalf("абонент прошёл техработы: %d %s", hits, render(calls))
	}
	e.press(t, owner, 5, "hit")
	if hits != 1 {
		t.Fatal("владелец не прошёл техработы")
	}
}
