package install

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const goodToken = "123456789:AAHdqTcvCH1vGWJxfSeofSAs0K5PALDsaw"

// ask запускает фрагмент, у которого вопросы читают ответы answers (по строке на ответ). Проверки сети подменены:
// токен годен только goodToken, панель отвечает кодом по адресу, DNS отдаёт заданные адреса.
func ask(t *testing.T, answers []string, script string) (string, error) {
	t.Helper()
	tty := filepath.Join(t.TempDir(), "tty")
	if err := os.WriteFile(tty, []byte(strings.Join(answers, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	stubs := `
tg_get_me() { [ "$1" = ` + goodToken + ` ] && echo workerbot; }
panel_status() {
  case "$1:$2" in
    https://panel.example.com:goodpanel) echo 200 ;;
    https://panel.example.com:*) echo 401 ;;
    *) echo 000 ;;
  esac
}
resolve_a() { case "$1" in bot.example.com) echo 203.0.113.5 ;; moved.example.com) echo 198.51.100.9 ;; esac; }
server_ips() { echo 203.0.113.5; }
external_ip() { echo; }
open_input
`
	return bash(t, []string{"WORKER_INSTALL_TTY=" + tty}, stubs+script)
}

func TestAskAll(t *testing.T) {
	out, err := ask(t, []string{
		"не токен", // формат
		"999999999:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", // формат верный, Telegram не принял
		goodToken,
		"abc", "0", "-5", "42", // ID
		"https://bot.example.com", "nodns.example.com", "moved.example.com", "bot.EXAMPLE.com", // домен
		"panel.example.com", "x", // адрес без схемы
		"http://panel.example.com", "x", // http к чужому хосту
		"https://panel.example.com", "wrong", // токен панели не принят
		"https://panel.example.com/", "goodpanel",
	}, `ask_all
echo "RESULT $BOT_USERNAME|$OWNER_ID|$DOMAIN|$PANEL_URL|${#SECRET_KEY}"`)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	for _, want := range []string{
		"Это не похоже на токен бота", "Токен не принят Telegram", "Telegram ID это число",
		"Нужно имя домена", "У домена nodns.example.com нет A-записи", "смотрит на 198.51.100.9",
		"Нужен полный адрес панели", "открытым текстом", "токен не принят (код 401)",
		"RESULT workerbot|42|bot.example.com|https://panel.example.com|44",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("в выводе нет %q\n%s", want, out)
		}
	}
	for _, secret := range []string{goodToken, "goodpanel", "wrong"} {
		if strings.Contains(out, secret) {
			t.Errorf("секрет %q попал на экран", secret)
		}
	}
}

func TestAskAllAbortsOnEOF(t *testing.T) {
	out, err := ask(t, []string{"плохо"}, "ask_all")
	if err == nil || !strings.Contains(out, "ввод оборвался") {
		t.Fatalf("конец ввода должен останавливать установку: %v\n%s", err, out)
	}
}

func TestInsideHost(t *testing.T) {
	for host, want := range map[string]bool{
		"localhost": true, "127.0.0.1": true, "10.1.2.3": true, "192.168.0.5": true, "172.16.0.1": true,
		"172.31.9.9": true, "panel": true, "172.32.0.1": false, "panel.example.com": false, "8.8.8.8": false,
	} {
		_, err := bash(t, nil, "inside_host "+host)
		if (err == nil) != want {
			t.Errorf("inside_host %s: ждали %v, получили err=%v", host, want, err)
		}
	}
}

func TestWaitHealth(t *testing.T) {
	// Двух ответов подряд достаточно; один и обрыв не считается.
	if out, err := bash(t, nil, `sleep() { :; }; n=0; health_ok() { n=$((n+1)); [ "$n" -ge 3 ]; }; wait_health`); err != nil {
		t.Fatalf("поднявшаяся служба не признана: %v\n%s", err, out)
	}
	if _, err := bash(t, nil, `sleep() { :; }; n=0; health_ok() { n=$((n+1)); [ $((n%2)) -eq 1 ]; }; wait_health`); err == nil {
		t.Fatal("мигающая служба признана живой")
	}
	if _, err := bash(t, nil, `sleep() { :; }; health_ok() { return 1; }; wait_health`); err == nil {
		t.Fatal("мёртвая служба признана живой")
	}
}

func TestShowKey(t *testing.T) {
	out, err := ask(t, []string{""}, `SECRET_KEY=ключ-для-теста; show_key`)
	if err != nil || strings.Count(out, "ключ-для-теста") != 1 || !strings.Contains(out, "больше установщик его не покажет") {
		t.Fatalf("ключ должен быть показан один раз с пояснением: %v\n%s", err, out)
	}
}
