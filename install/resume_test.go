package install

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Заглушки шагов установки: каждый записывает своё имя. Файлы установки лежат во временной папке.
func resumeStubs(t *testing.T) (script, etc string) {
	t.Helper()
	etc = t.TempDir()
	return `
ETC_DIR=` + etc + `; ENV_FILE=` + etc + `/env; DONE_FILE=` + etc + `/.install-done
step() { echo "STEP $1" >>` + etc + `/steps.log; }
open_input() { step open_input; }
ask_all() { step ask_all; BOT_USERNAME=bot; DOMAIN=bot.example.com; }
show_key() { step show_key; }
install_binary() { step install_binary; }
setup_user_dirs() { step setup_user_dirs; }
install_unit() { step install_unit; }
write_env() { step write_env; }
systemctl() { step "systemctl $1"; }
wait_health() { step wait_health; }
setup_https() { step setup_https; HTTPS_DONE=1; }
set_owner_password() { step set_owner_password; }
tg_get_me() { echo botname; }
read_answer() { ANSWER=n; }
`, etc
}

// steps отдаёт журнал шагов вместе с выводом скрипта.
func steps(etc, out string) string {
	b, _ := os.ReadFile(filepath.Join(etc, "steps.log"))
	return out + string(b)
}

func TestRunInstallFresh(t *testing.T) {
	stubs, etc := resumeStubs(t)
	out, err := bash(t, nil, stubs+`run_install 0 /бинарник`)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	out = steps(etc, out)
	order := []string{"ask_all", "show_key", "install_binary", "setup_user_dirs", "install_unit", "write_env", "systemctl enable", "wait_health", "setup_https", "set_owner_password"}
	pos := -1
	for _, step := range order {
		i := strings.Index(out, "STEP "+step)
		if i < pos || i < 0 {
			t.Fatalf("шаг %q не на месте:\n%s", step, out)
		}
		pos = i
	}
	if _, err := os.Stat(filepath.Join(etc, ".install-done")); err != nil {
		t.Error("метка завершения не создана")
	}
}

// Оборвалось после write_env: повторный запуск не спрашивает, не пишет настройки заново и доводит остальное.
func TestRunInstallResume(t *testing.T) {
	stubs, etc := resumeStubs(t)
	_ = os.WriteFile(filepath.Join(etc, "env"), []byte("PUBLIC_URL=https://bot.example.com\nBOT_TOKEN=123456789:AAHdqTcvCH1vGWJxfSeofSAs0K5PALDsaw\nSECRET_KEY=старый\n"), 0o640)
	out, err := bash(t, nil, stubs+`run_install 1 /бинарник; echo "DOMAIN=$DOMAIN"`)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	out = steps(etc, out)
	for _, bad := range []string{"STEP ask_all", "STEP show_key", "STEP write_env", "STEP set_owner_password"} {
		if strings.Contains(out, bad) {
			t.Errorf("при продолжении выполнено %q\n%s", bad, out)
		}
	}
	for _, want := range []string{"STEP install_binary", "STEP install_unit", "STEP systemctl enable", "STEP wait_health", "STEP setup_https", "DOMAIN=bot.example.com"} {
		if !strings.Contains(out, want) {
			t.Errorf("при продолжении нет %q\n%s", want, out)
		}
	}
	if b, _ := os.ReadFile(filepath.Join(etc, "env")); !strings.Contains(string(b), "SECRET_KEY=старый") {
		t.Error("SECRET_KEY перезаписан")
	}
	if _, err := os.Stat(filepath.Join(etc, ".install-done")); err != nil {
		t.Error("метка завершения не создана")
	}
}

// Служба не поднялась: метки нет, значит, следующий запуск продолжит.
func TestRunInstallFailureLeavesNoMarker(t *testing.T) {
	stubs, etc := resumeStubs(t)
	out, err := bash(t, nil, stubs+`wait_health() { return 1; }; run_install 0 /бинарник`)
	if err == nil || !strings.Contains(out, "продолжит с этого места") {
		t.Fatalf("сбой должен останавливать и подсказывать повторный запуск: %v\n%s", err, out)
	}
	if _, err := os.Stat(filepath.Join(etc, ".install-done")); err == nil {
		t.Error("метка создана после сбоя")
	}
	if strings.Contains(steps(etc, out), "STEP setup_https") {
		t.Error("https настраивался после сбоя службы")
	}
}

// main: есть env и метка, значит, всё сделано; есть env без метки, значит, продолжение.
func TestMainDecidesResume(t *testing.T) {
	s := scriptText(t)
	for _, want := range []string{`[ -e "$DONE_FILE" ]`, "resume=1", `run_install "$resume"`} {
		if !strings.Contains(s, want) {
			t.Errorf("в main нет %q", want)
		}
	}
}
