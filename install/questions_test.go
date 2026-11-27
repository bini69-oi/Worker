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
		"localhost": true, "127.0.0.1": true, "127.5.5.5": true, "10.1.2.3": true, "192.168.0.5": true, "172.16.0.1": true,
		"172.31.9.9": true, "panel": true, "::1": true, "fd12:3456::1": true,
		"172.32.0.1": false, "172.15.0.1": false, "192.169.0.1": false, "11.0.0.1": false, "panel.example.com": false,
		"8.8.8.8": false, "10.example.com": false, "127.0.0.1.evil.com": false, "10.0.0.256": false, "010.0.0.1": false,
		"10.0.0": false, "2001:db8::1": false, "fe80::1": false, "1.2.3.4.5": false,
	} {
		_, err := bash(t, nil, "inside_host '"+host+"'")
		if (err == nil) != want {
			t.Errorf("inside_host %s: ждали %v, получили err=%v", host, want, err)
		}
	}
}

// Хост берётся так же, как в Go (u.Hostname()): userinfo, порт, путь и фрагмент не обманывают проверку.
func TestURLHostAndInside(t *testing.T) {
	cases := []struct {
		url, host string
		inside    bool
	}{
		{"http://127.0.0.1:3000", "127.0.0.1", true},
		{"http://user:pw@10.0.0.5/api", "10.0.0.5", true},
		{"http://[::1]:3000", "::1", true},
		{"http://panel:3000/x", "panel", true},
		{"http://localhost", "localhost", true},
		{"http://10.example.com", "10.example.com", false},
		{"http://127.0.0.1@panel.example.com", "panel.example.com", false},
		{"http://127.0.0.1:80@panel.example.com:3000/", "panel.example.com", false},
		{"http://localhost@evil.com", "evil.com", false},
		{"http://a@127.0.0.1@evil.com", "evil.com", false},
		{"http://panel.example.com#@127.0.0.1", "panel.example.com", false},
		{"http://panel.example.com?x=@127.0.0.1", "panel.example.com", false},
		{"http://panel.example.com/@127.0.0.1", "panel.example.com", false},
		{"http://8.8.8.8", "8.8.8.8", false},
	}
	for _, c := range cases {
		out, err := bash(t, nil, "url_host '"+c.url+"'")
		if err != nil || strings.TrimSpace(out) != c.host {
			t.Errorf("url_host %s = %q, ждали %q", c.url, out, c.host)
		}
		_, err = bash(t, nil, "inside_host \"$(url_host '"+c.url+"')\"")
		if (err == nil) != c.inside {
			t.Errorf("%s: внутренний=%v, ждали %v", c.url, err == nil, c.inside)
		}
	}
}

// check_panel не пускает http к чужому хосту, как бы адрес ни маскировали, и не доходит до запроса к панели.
func TestCheckPanelHTTPDisguised(t *testing.T) {
	for _, url := range []string{"http://10.example.com", "http://127.0.0.1@panel.example.com", "http://panel.example.com#@127.0.0.1"} {
		out, err := bash(t, nil, "panel_status() { echo REQUESTED; echo 200; }; check_panel '"+url+"' tok")
		if err == nil || strings.Contains(out, "REQUESTED") || !strings.Contains(out, "открытым текстом") {
			t.Errorf("%s принят: %v\n%s", url, err, out)
		}
	}
	if out, err := bash(t, nil, "panel_status() { echo 200; }; check_panel 'http://127.0.0.1:3000' tok"); err != nil {
		t.Errorf("локальная панель по http отклонена: %v\n%s", err, out)
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
