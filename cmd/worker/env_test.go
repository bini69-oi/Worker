package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testKey = "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=" // 32 байта в base64

func goodFile() string {
	return `# пример
BOT_TOKEN=123:secret-bot-token
OWNER_TG_ID=42
PUBLIC_URL=https://bot.example.com/
PANEL_URL="http://127.0.0.1:3000"
PANEL_TOKEN='secret-panel-token'
SECRET_KEY=` + testKey + "\n"
}

func writeEnv(t *testing.T, body string, mode os.FileMode) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(path, []byte(body), mode); err != nil {
		t.Fatal(err)
	}
	return path
}

func noEnv(string) string { return "" }

func TestLoadEnvGood(t *testing.T) {
	env, warns, err := loadEnv(writeEnv(t, goodFile(), 0o600), noEnv)
	if err != nil {
		t.Fatal(err)
	}
	if len(warns) != 0 {
		t.Fatalf("лишние предупреждения: %v", warns)
	}
	if env.OwnerID != 42 || env.PublicURL != "https://bot.example.com" || env.PanelURL != "http://127.0.0.1:3000" {
		t.Fatalf("неверно прочитано: %+v", env)
	}
	if env.PanelToken != "secret-panel-token" || len(env.SecretKey) != 32 || env.DBPath != defaultDBPath {
		t.Fatalf("неверно прочитано: %+v", env)
	}
}

func TestLoadEnvMissingToken(t *testing.T) {
	body := strings.Replace(goodFile(), "BOT_TOKEN=123:secret-bot-token\n", "", 1)
	_, _, err := loadEnv(writeEnv(t, body, 0o600), noEnv)
	if err == nil || !strings.Contains(err.Error(), "BOT_TOKEN") {
		t.Fatalf("ждали ошибку про BOT_TOKEN, получили %v", err)
	}
}

func TestLoadEnvExtraVariable(t *testing.T) {
	body := goodFile() + "TRIAL_DAYS=3\n"
	_, warns, err := loadEnv(writeEnv(t, body, 0o600), noEnv)
	if err != nil {
		t.Fatal(err)
	}
	if len(warns) != 1 || !strings.Contains(warns[0], "TRIAL_DAYS") {
		t.Fatalf("ждали предупреждение про TRIAL_DAYS, получили %v", warns)
	}
}

func TestLoadEnvProcessWins(t *testing.T) {
	getenv := func(k string) string {
		if k == "OWNER_TG_ID" {
			return "777"
		}
		return ""
	}
	env, _, err := loadEnv(writeEnv(t, goodFile(), 0o600), getenv)
	if err != nil {
		t.Fatal(err)
	}
	if env.OwnerID != 777 {
		t.Fatalf("переменная процесса должна быть важнее файла, OwnerID=%d", env.OwnerID)
	}
}

func TestLoadEnvNoFile(t *testing.T) {
	_, _, err := loadEnv(filepath.Join(t.TempDir(), "нет.env"), noEnv)
	if err == nil || !strings.Contains(err.Error(), "не заданы") {
		t.Fatalf("ждали список недостающих, получили %v", err)
	}
}

func TestLoadEnvWideRights(t *testing.T) {
	_, warns, err := loadEnv(writeEnv(t, goodFile(), 0o644), noEnv)
	if err != nil {
		t.Fatal(err)
	}
	if len(warns) != 1 || !strings.Contains(warns[0], "chmod 600") {
		t.Fatalf("ждали предупреждение о правах, получили %v", warns)
	}
}

func TestLoadEnvBadValues(t *testing.T) {
	cases := map[string][2]string{
		"короткий ключ":                {"SECRET_KEY=" + testKey, "SECRET_KEY=c2hvcnQ="},
		"владелец не число":            {"OWNER_TG_ID=42", "OWNER_TG_ID=admin"},
		"публичный без https":          {"PUBLIC_URL=https://bot.example.com/", "PUBLIC_URL=http://bot.example.com"},
		"панель без схемы":             {`PANEL_URL="http://127.0.0.1:3000"`, "PANEL_URL=panel.example.com"},
		"панель по http извне":         {`PANEL_URL="http://127.0.0.1:3000"`, "PANEL_URL=http://panel.example.com"},
		"панель по http на внешний IP": {`PANEL_URL="http://127.0.0.1:3000"`, "PANEL_URL=http://203.0.113.5:3000"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			body := strings.Replace(goodFile(), c[0], c[1], 1)
			_, _, err := loadEnv(writeEnv(t, body, 0o600), noEnv)
			if err == nil {
				t.Fatal("ждали ошибку")
			}
		})
	}
}

func TestLoadEnvErrorsHaveNoSecrets(t *testing.T) {
	body := strings.Replace(goodFile(), "SECRET_KEY="+testKey, "SECRET_KEY=secret-bad-key", 1)
	_, _, err := loadEnv(writeEnv(t, body, 0o600), noEnv)
	if err == nil || strings.Contains(err.Error(), "secret") {
		t.Fatalf("в ошибке не должно быть значений: %v", err)
	}
}

func TestParseEnvBadLine(t *testing.T) {
	if _, err := parseEnv(strings.NewReader("BOT_TOKEN\n")); err == nil {
		t.Fatal("ждали ошибку про строку без =")
	}
}

func TestPanelHTTPInside(t *testing.T) {
	for _, u := range []string{"http://127.0.0.1:3000", "http://10.0.0.5:3000", "http://localhost:3000", "http://remnawave:3000", "https://panel.example.com"} {
		if _, err := cleanURL(u, false); err != nil {
			t.Errorf("%s: %v", u, err)
		}
	}
}

func TestEnvPath(t *testing.T) {
	if got := envPath(func(string) string { return "" }); got != ".env" {
		t.Errorf("по умолчанию %q", got)
	}
	if got := envPath(func(k string) string {
		if k == "WORKER_ENV_FILE" {
			return "/etc/worker/env"
		}
		return ""
	}); got != "/etc/worker/env" {
		t.Errorf("с переменной %q", got)
	}
}

func TestLoadEnvHTTPAddr(t *testing.T) {
	env, warns, err := loadEnv(writeEnv(t, goodFile()+"HTTP_ADDR=127.0.0.1:8080\n", 0o600), noEnv)
	if err != nil || len(warns) != 0 || env.HTTPAddr != "127.0.0.1:8080" {
		t.Fatalf("HTTP_ADDR не прочитан: %+v %v %v", env, warns, err)
	}
	env, _, _ = loadEnv(writeEnv(t, goodFile(), 0o600), noEnv)
	if env.HTTPAddr != "" {
		t.Errorf("без переменной адрес должен быть пустым: %q", env.HTTPAddr)
	}
	for _, bad := range []string{"8080", "127.0.0.1", "host:port", "127.0.0.1:99999"} {
		if _, _, err := loadEnv(writeEnv(t, goodFile()+"HTTP_ADDR="+bad+"\n", 0o600), noEnv); err == nil {
			t.Errorf("HTTP_ADDR=%s принят", bad)
		}
	}
}

func TestListenAddr(t *testing.T) {
	if got := listenAddr(Env{HTTPAddr: "127.0.0.1:8080"}, ":9999"); got != "127.0.0.1:8080" {
		t.Errorf("HTTP_ADDR должен быть главнее настройки: %q", got)
	}
	if got := listenAddr(Env{}, ":9999"); got != ":9999" {
		t.Errorf("без HTTP_ADDR адрес из настройки: %q", got)
	}
}
