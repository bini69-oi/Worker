package install

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Caddyfile в установщике и в Caddyfile.tmpl один и тот же.
func TestScriptCaddyMatchesTemplate(t *testing.T) {
	file, err := os.ReadFile("Caddyfile.tmpl")
	if err != nil {
		t.Fatal(err)
	}
	embedded := between(t, scriptText(t), "<<'CADDY'\n", "\nCADDY\n")
	if strings.TrimSpace(embedded) != strings.TrimSpace(string(file)) {
		t.Fatal("Caddyfile в install.sh не совпадает с install/Caddyfile.tmpl")
	}
}

func TestScriptPortBusy(t *testing.T) {
	// Список ss -ltnH: адреса и порты. 8080 и 5432 не считаются портами 80 и 443.
	listing := `ss() { printf '%s\n' "LISTEN 0 511 0.0.0.0:443 0.0.0.0:*" "LISTEN 0 4096 127.0.0.1:8080 0.0.0.0:*" "LISTEN 0 128 [::]:5432 [::]:*"; }`
	if _, err := bash(t, nil, listing+"; port_busy 443"); err != nil {
		t.Error("443 слушается, а занятым не признан")
	}
	if _, err := bash(t, nil, listing+"; port_busy 80"); err == nil {
		t.Error("80 свободен, а признан занятым (спутан с 8080)")
	}
	v6 := `ss() { printf '%s\n' "LISTEN 0 511 [::]:80 [::]:*"; }`
	if _, err := bash(t, nil, v6+"; port_busy 80"); err != nil {
		t.Error("80 на IPv6 не замечен")
	}
}

const stubs = `
install_caddy() { echo CADDY-INSTALLED; }
systemctl() { echo "systemctl $*"; }
apt-get() { echo APT-CALLED; }
DOMAIN=bot.example.com
`

// Заняты порты: Caddy не ставится, на экране конфиг nginx с проксированием на 127.0.0.1:8080, файлы не тронуты.
func TestSetupHTTPSBusyPorts(t *testing.T) {
	caddyfile := filepath.Join(t.TempDir(), "Caddyfile")
	out, err := bash(t, nil, stubs+`CADDYFILE=`+caddyfile+`; port_busy() { [ "$1" = 443 ]; }; caddy_present() { return 1; }; setup_https`)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	for _, want := range []string{
		"server_name bot.example.com;", "proxy_pass http://127.0.0.1:8080;", "proxy_set_header X-Real-IP", "nginx -t",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("в подсказке нет %q\n%s", want, out)
		}
	}
	for _, bad := range []string{"CADDY-INSTALLED", "APT-CALLED", "systemctl"} {
		if strings.Contains(out, bad) {
			t.Errorf("при занятых портах выполнено %q\n%s", bad, out)
		}
	}
	if _, err := os.Stat(caddyfile); err == nil {
		t.Error("Caddyfile записан при занятых портах")
	}
}

func TestSetupHTTPSForeignCaddy(t *testing.T) {
	caddyfile := filepath.Join(t.TempDir(), "Caddyfile")
	_ = os.WriteFile(caddyfile, []byte("чужой конфиг\n"), 0o644)
	out, err := bash(t, nil, stubs+`CADDYFILE=`+caddyfile+`; port_busy() { return 1; }; caddy_present() { return 0; }; setup_https`)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if strings.Contains(out, "CADDY-INSTALLED") || !strings.Contains(out, "уже стоит Caddy") {
		t.Errorf("чужой Caddy должен остаться как есть:\n%s", out)
	}
	if b, _ := os.ReadFile(caddyfile); string(b) != "чужой конфиг\n" {
		t.Error("чужой Caddyfile изменён")
	}
}

func TestSetupHTTPSInstallsCaddy(t *testing.T) {
	caddyfile := filepath.Join(t.TempDir(), "Caddyfile")
	out, err := bash(t, nil, stubs+`CADDYFILE=`+caddyfile+`; port_busy() { return 1; }; caddy_present() { return 1; }; setup_https; echo "DONE=$HTTPS_DONE"`)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if !strings.Contains(out, "CADDY-INSTALLED") || !strings.Contains(out, "DONE=1") || !strings.Contains(out, "systemctl restart caddy") {
		t.Errorf("Caddy не настроен:\n%s", out)
	}
	b, _ := os.ReadFile(caddyfile)
	if !strings.Contains(string(b), "bot.example.com {") || !strings.Contains(string(b), "reverse_proxy 127.0.0.1:8080") ||
		strings.Contains(string(b), "{{DOMAIN}}") {
		t.Errorf("Caddyfile: %q", b)
	}
}
