package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// Файл настроек, который пишет install.sh, читается загрузчиком Worker, а права на нём 600.
func TestInstallScriptEnvLoads(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("нет bash")
	}
	script, _ := filepath.Abs("../../install/install.sh")
	etc := t.TempDir()
	run := `. ` + script + `
chown() { :; }
ETC_DIR=` + etc + `; ENV_FILE=` + etc + `/env; DATA_DIR=/var/lib/worker
BOT_TOKEN=123456789:AAHdqTcvCH1vGWJxfSeofSAs0K5PALDsaw; OWNER_ID=42; DOMAIN=bot.example.com
PANEL_URL=https://panel.example.com; PANEL_TOKEN='tok#en=1'; SECRET_KEY=$(openssl rand -base64 32)
write_env`
	cmd := exec.Command("bash", "-c", "set -euo pipefail\n"+run)
	cmd.Env = append(os.Environ(), "WORKER_INSTALL_SOURCE_ONLY=1")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	file := filepath.Join(etc, "env")
	if st, err := os.Stat(file); err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("права файла настроек: %v %v", st, err)
	}
	env, warns, err := loadEnv(file, func(string) string { return "" })
	if err != nil {
		t.Fatalf("загрузчик не принял файл установщика: %v", err)
	}
	if len(warns) != 0 {
		t.Errorf("предупреждения: %v", warns)
	}
	if env.PublicURL != "https://bot.example.com" || env.OwnerID != 42 || env.PanelToken != "tok#en=1" ||
		env.HTTPAddr != "127.0.0.1:8080" || env.DBPath != "/var/lib/worker/worker.db" || len(env.SecretKey) != 32 {
		t.Errorf("прочитано не то: %+v", env)
	}
	left, _ := filepath.Glob(filepath.Join(etc, ".env.*"))
	if len(left) != 0 {
		t.Errorf("остались временные файлы: %v", left)
	}
}
