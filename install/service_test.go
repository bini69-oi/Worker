package install

import (
	"os"
	"strings"
	"testing"
)

// unit разбирает файл службы в список "раздел: ключ=значение" без учёта комментариев.
func unit(t *testing.T) map[string][]string {
	t.Helper()
	b, err := os.ReadFile("worker.service")
	if err != nil {
		t.Fatal(err)
	}
	out := map[string][]string{}
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "[") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			t.Fatalf("строка не вида ключ=значение: %q", line)
		}
		out[k] = append(out[k], v)
	}
	return out
}

// Песочница из design.md. Оценка systemd-analyze security проверяется на сервере (tasks 4.1), здесь только состав.
func TestServiceSandbox(t *testing.T) {
	u := unit(t)
	want := map[string]string{
		"User": "worker", "Group": "worker", "ExecStart": "/usr/local/bin/worker",
		"WorkingDirectory": "/var/lib/worker", "EnvironmentFile": "/etc/worker/env",
		"NoNewPrivileges": "true", "ProtectSystem": "strict", "ReadWritePaths": "/var/lib/worker", "InaccessiblePaths": "/etc/worker",
		"ProtectHome": "true", "PrivateTmp": "true", "PrivateDevices": "true",
		"ProtectKernelTunables": "true", "ProtectKernelModules": "true", "ProtectControlGroups": "true",
		"RestrictNamespaces": "true", "LockPersonality": "true", "MemoryDenyWriteExecute": "true",
		"RestrictAddressFamilies": "AF_INET AF_INET6 AF_UNIX", "CapabilityBoundingSet": "",
		"UMask": "0077", "Restart": "on-failure",
	}
	for k, v := range want {
		got, ok := u[k]
		if !ok || len(got) == 0 || got[0] != v {
			t.Errorf("%s: ждали %q, есть %q", k, v, got)
		}
	}
	if sf := strings.Join(u["SystemCallFilter"], " "); !strings.Contains(sf, "@system-service") {
		t.Errorf("SystemCallFilter без @system-service: %q", sf)
	}
}

// Ничего, что снимает песочницу или даёт права.
func TestServiceNoPrivileges(t *testing.T) {
	u := unit(t)
	for _, bad := range []string{"AmbientCapabilities", "SupplementaryGroups", "PermissionsStartOnly"} {
		for _, v := range u[bad] {
			if v != "" {
				t.Errorf("%s=%s даёт лишнее", bad, v)
			}
		}
	}
	for _, k := range []string{"User", "Group"} {
		if len(u[k]) > 0 && u[k][0] == "root" {
			t.Errorf("%s=root", k)
		}
	}
	for _, k := range []string{"PrivateNetwork", "ProtectSystem"} {
		for _, v := range u[k] {
			if v == "false" || v == "no" {
				t.Errorf("%s=%s ослабляет песочницу", k, v)
			}
		}
	}
}
