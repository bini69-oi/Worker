package install

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bini69-oi/worker/release"
)

func scriptText(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("install.sh")
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// between возвращает текст между строками start и end (сами строки не входят).
func between(t *testing.T, s, start, end string) string {
	t.Helper()
	i := strings.Index(s, start)
	if i < 0 {
		t.Fatalf("в install.sh нет %q", start)
	}
	rest := s[i+len(start):]
	j := strings.Index(rest, end)
	if j < 0 {
		t.Fatalf("в install.sh нет %q после %q", end, start)
	}
	return rest[:j]
}

// bash запускает фрагмент с подключёнными функциями установщика. Нужны bash и openssl, иначе тест пропускается.
func bash(t *testing.T, env []string, script string) (string, error) {
	t.Helper()
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("нет bash")
	}
	abs, _ := filepath.Abs("install.sh")
	cmd := exec.Command("bash", "-c", "set -euo pipefail; . "+abs+"\n"+script)
	cmd.Env = append(os.Environ(), append([]string{"WORKER_INSTALL_SOURCE_ONLY=1"}, env...)...)
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	err := cmd.Run()
	return out.String(), err
}

func TestScriptShellcheck(t *testing.T) {
	sc, err := exec.LookPath("shellcheck")
	if err != nil {
		t.Skip("shellcheck не установлен (в CI он есть)")
	}
	if out, err := exec.Command(sc, "install.sh").CombinedOutput(); err != nil {
		t.Fatalf("shellcheck:\n%s", out)
	}
}

// Ключ в установщике и в release.pub один и тот же: иначе установщик и worker update доверяли бы разным ключам.
func TestScriptKeyMatchesReleasePub(t *testing.T) {
	pub, err := os.ReadFile("release.pub")
	if err != nil {
		t.Fatal(err)
	}
	embedded := between(t, scriptText(t), "RELEASE_KEY='", "'\n# END RELEASE KEY")
	if strings.TrimSpace(embedded) != strings.TrimSpace(string(pub)) {
		t.Fatal("ключ в install.sh не совпадает с install/release.pub")
	}
}

// Служба в установщике и в worker.service одна и та же.
func TestScriptUnitMatchesServiceFile(t *testing.T) {
	file, err := os.ReadFile("worker.service")
	if err != nil {
		t.Fatal(err)
	}
	embedded := between(t, scriptText(t), "<<'UNIT'\n", "\nUNIT\n")
	if strings.TrimSpace(embedded) != strings.TrimSpace(string(file)) {
		t.Fatal("служба в install.sh не совпадает с install/worker.service")
	}
}

func TestScriptCheckOS(t *testing.T) {
	cases := []struct {
		name, release string
		ok            bool
	}{
		{"debian 12", "ID=debian\nVERSION_ID=\"12\"\n", true},
		{"debian 11", "ID=debian\nVERSION_ID=\"11\"\n", true},
		{"debian 10", "ID=debian\nVERSION_ID=\"10\"\n", false},
		{"ubuntu 24.04", "ID=ubuntu\nVERSION_ID=\"24.04\"\n", true},
		{"ubuntu 22.04", "ID=ubuntu\nVERSION_ID=\"22.04\"\n", true},
		{"ubuntu 20.04", "ID=ubuntu\nVERSION_ID=\"20.04\"\n", false},
		{"centos", "ID=centos\nVERSION_ID=\"9\"\n", false},
		{"debian без версии", "ID=debian\n", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := filepath.Join(t.TempDir(), "os-release")
			_ = os.WriteFile(f, []byte(c.release), 0o644)
			out, err := bash(t, []string{"WORKER_OS_RELEASE=" + f}, "check_os")
			if (err == nil) != c.ok {
				t.Fatalf("ждали ok=%v, получили err=%v\n%s", c.ok, err, out)
			}
		})
	}
	if _, err := bash(t, []string{"WORKER_OS_RELEASE=/нет/такого"}, "check_os"); err == nil {
		t.Error("без os-release проверка должна отказывать")
	}
}

func TestScriptDetectArch(t *testing.T) {
	for in, want := range map[string]string{"x86_64": "amd64", "aarch64": "arm64", "arm64": "arm64"} {
		out, err := bash(t, nil, "machine() { echo "+in+"; }; detect_arch")
		if err != nil || strings.TrimSpace(out) != want {
			t.Errorf("%s: %q %v", in, out, err)
		}
	}
	if _, err := bash(t, nil, "machine() { echo armv7l; }; detect_arch"); err == nil {
		t.Error("armv7l принят")
	}
}

// releaseDir готовит папку релиза: бинарник, суммы и подпись.
func releaseDir(t *testing.T, bin string) (dir string, pubPEM []byte) {
	t.Helper()
	priv, pub, err := release.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	dir = t.TempDir()
	sum := sha256.Sum256([]byte(bin))
	sums := hex.EncodeToString(sum[:]) + "  worker-linux-amd64\n"
	for name, body := range map[string][]byte{
		"worker-linux-amd64": []byte(bin), "SHA256SUMS": []byte(sums), "SHA256SUMS.sig": release.Sign(priv, []byte(sums)),
	} {
		if err := os.WriteFile(filepath.Join(dir, name), body, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir, pub
}

func requireOpenSSL(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("openssl"); err != nil {
		t.Skip("нет openssl")
	}
}

func verify(t *testing.T, dir string, key []byte) (string, error) {
	t.Helper()
	work := t.TempDir()
	for _, f := range []string{"worker-linux-amd64", "SHA256SUMS", "SHA256SUMS.sig"} {
		b, err := os.ReadFile(filepath.Join(dir, f))
		if err == nil {
			_ = os.WriteFile(filepath.Join(work, f), b, 0o644)
		}
	}
	keyFile := filepath.Join(t.TempDir(), "key")
	_ = os.WriteFile(keyFile, key, 0o644)
	return bash(t, nil, `RELEASE_KEY="$(cat `+keyFile+`)"; verify_release `+work+` amd64`)
}

func TestScriptVerifyRelease(t *testing.T) {
	requireOpenSSL(t)
	dir, pub := releaseDir(t, "бинарник")
	if out, err := verify(t, dir, pub); err != nil {
		t.Fatalf("верный релиз не принят: %v\n%s", err, out)
	}

	// Подпись другого ключа.
	_, otherPub, _ := release.GenerateKey()
	if out, err := verify(t, dir, otherPub); err == nil || !strings.Contains(out, "подпись") {
		t.Errorf("подпись чужого ключа принята: %v\n%s", err, out)
	}

	// Бинарник подменён после подписи.
	_ = os.WriteFile(filepath.Join(dir, "worker-linux-amd64"), []byte("подмена"), 0o644)
	if out, err := verify(t, dir, pub); err == nil || !strings.Contains(out, "сумма") {
		t.Errorf("подменённый бинарник принят: %v\n%s", err, out)
	}

	// Список сумм изменён: подпись не сходится.
	dir2, pub2 := releaseDir(t, "бинарник")
	_ = os.WriteFile(filepath.Join(dir2, "SHA256SUMS"), []byte(strings.Repeat("0", 64)+"  worker-linux-amd64\n"), 0o644)
	if out, err := verify(t, dir2, pub2); err == nil || !strings.Contains(out, "подпись") {
		t.Errorf("изменённый список принят: %v\n%s", err, out)
	}

	// Подписи нет вовсе.
	dir3, pub3 := releaseDir(t, "бинарник")
	_ = os.Remove(filepath.Join(dir3, "SHA256SUMS.sig"))
	if _, err := verify(t, dir3, pub3); err == nil {
		t.Error("релиз без подписи принят")
	}
}

func TestScriptVerifyStubKeyRefuses(t *testing.T) {
	requireOpenSSL(t)
	dir, _ := releaseDir(t, "бинарник")
	stub, _ := os.ReadFile("release.pub")
	out, err := verify(t, dir, stub)
	if err == nil || !strings.Contains(out, "нет ключа") {
		t.Fatalf("с заглушкой установка должна останавливаться: %v\n%s", err, out)
	}
}

// Установка до проверки ничего не ставит: проверка стоит в main раньше install_binary.
func TestScriptVerifyBeforeInstall(t *testing.T) {
	s := scriptText(t)
	main := s[strings.Index(s, "main() {"):]
	if strings.Index(main, "verify_release \"$tmp\"") > strings.Index(main, "install_binary \"$tmp") {
		t.Fatal("бинарник ставится раньше проверки подписи")
	}
	if strings.Index(main, "download_release") > strings.Index(main, "verify_release") {
		t.Fatal("проверка раньше скачивания")
	}
}

func TestScriptRejectsBadArgs(t *testing.T) {
	for _, args := range []string{"--version", "--version 1.2", "--version 'v1.2.3; rm -rf /'", "--nope"} {
		out, err := bash(t, []string{"WORKER_OS_RELEASE=/нет"}, "main "+args)
		if err == nil {
			t.Errorf("аргументы %q приняты: %s", args, out)
		}
	}
}
