package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bini69-oi/worker/release"
)

const oldBin = "старый бинарник v1"

// fakeReleases это сервер релизов GitHub: /latest/download/<файл> и /download/<тег>/<файл>.
type fakeReleases struct {
	srv   *httptest.Server
	priv  ed25519.PrivateKey
	pub   []byte
	files map[string][]byte // имя файла в релизе -> содержимое
	// version это тег, который подписан внутри SHA256SUMS. Сервер отдаёт релиз под тегом v1.2.3.
	version string
	hits    []string
	mu      sync.Mutex
}

func newReleases(t *testing.T, bin string) *fakeReleases {
	t.Helper()
	priv, pub, err := release.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeReleases{priv: priv, pub: pub, files: map[string][]byte{}, version: "v1.2.3"}
	f.publish("worker-linux-amd64", []byte(bin))
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.hits = append(f.hits, r.URL.Path)
		f.mu.Unlock()
		if r.URL.Path == "/latest" {
			http.Redirect(w, r, "/tag/v1.2.3", http.StatusFound)
			return
		}
		name := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
		body, ok := f.files[name]
		if !ok || !strings.HasPrefix(r.URL.Path, "/download/v1.2.3/") {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(body)
	}))
	t.Cleanup(f.srv.Close)
	return f
}

// publish кладёт бинарник и пересчитывает подписанный список сумм.
func (f *fakeReleases) publish(name string, body []byte) {
	f.files[name] = body
	sum := sha256.Sum256(f.files["worker-linux-amd64"])
	sums := "version " + f.version + "\n" + hex.EncodeToString(sum[:]) + "  worker-linux-amd64\n"
	if b, ok := f.files["worker-linux-arm64"]; ok {
		s2 := sha256.Sum256(b)
		sums += hex.EncodeToString(s2[:]) + "  worker-linux-arm64\n"
	}
	f.files["SHA256SUMS"] = []byte(sums)
	f.files["SHA256SUMS.sig"] = release.Sign(f.priv, []byte(sums))
}

type upEnv struct {
	u      *updater
	bin    string
	events *[]string
	out    *bytes.Buffer
}

func newUpdater(t *testing.T, rel *fakeReleases) upEnv {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "worker")
	if err := os.WriteFile(bin, []byte(oldBin), 0o755); err != nil {
		t.Fatal(err)
	}
	var events []string
	out := &bytes.Buffer{}
	u := &updater{
		BaseURL: rel.srv.URL, Client: rel.srv.Client(), PubKey: rel.pub, Arch: "amd64", BinPath: bin, Out: out,
		Current: "v1.0.0",
		Backup: func(context.Context) (string, error) {
			events = append(events, "backup")
			b, _ := os.ReadFile(bin)
			if string(b) != oldBin {
				return "", errors.New("бэкап должен идти до замены бинарника")
			}
			return "/var/lib/worker/backups/x.db.enc", nil
		},
		Restart: func(context.Context) error { events = append(events, "restart"); return nil },
	}
	return upEnv{u: u, bin: bin, events: &events, out: out}
}

func content(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestUpdateReplacesBinary(t *testing.T) {
	rel := newReleases(t, "новый бинарник v2")
	e := newUpdater(t, rel)
	if err := e.u.Run(t.Context(), ""); err != nil {
		t.Fatal(err)
	}
	if content(t, e.bin) != "новый бинарник v2" {
		t.Fatal("бинарник не заменён")
	}
	if st, _ := os.Stat(e.bin); st.Mode().Perm() != 0o755 {
		t.Errorf("права бинарника %v", st.Mode())
	}
	if strings.Join(*e.events, ",") != "backup,restart" {
		t.Errorf("порядок шагов %v", *e.events)
	}
	for _, leftover := range []string{e.bin + ".new", e.bin + ".old"} {
		if _, err := os.Stat(leftover); err == nil {
			t.Errorf("остался %s", leftover)
		}
	}
}

func TestUpdateSpecificVersion(t *testing.T) {
	rel := newReleases(t, "новый бинарник v2")
	e := newUpdater(t, rel)
	if err := e.u.Run(t.Context(), "v1.2.3"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(rel.hits, " "), "/download/v1.2.3/worker-linux-amd64") {
		t.Errorf("скачивали не тот тег: %v", rel.hits)
	}
}

func TestUpdateBadVersionArg(t *testing.T) {
	rel := newReleases(t, "x")
	e := newUpdater(t, rel)
	for _, v := range []string{"1.2.3", "v1.2", "../x", "v1.2.3/../../evil", "latest"} {
		if err := e.u.Run(t.Context(), v); err == nil {
			t.Errorf("версия %q принята", v)
		}
	}
	if len(rel.hits) != 0 {
		t.Errorf("при плохой версии ходили в сеть: %v", rel.hits)
	}
}

// Всё, что не сошлось с подписью, останавливает обновление до бэкапа и замены.
func TestUpdateRefusals(t *testing.T) {
	cases := []struct {
		name   string
		break_ func(f *fakeReleases)
		want   string
	}{
		{"подпись чужим ключом", func(f *fakeReleases) {
			other, _, _ := release.GenerateKey()
			f.files["SHA256SUMS.sig"] = release.Sign(other, f.files["SHA256SUMS"])
		}, "подпись"},
		{"список изменён после подписи", func(f *fakeReleases) {
			f.files["SHA256SUMS"] = append(f.files["SHA256SUMS"], "\n"...)
		}, "подпись"},
		{"подписана другая версия", func(f *fakeReleases) {
			f.version = "v1.0.0"
			f.publish("worker-linux-amd64", f.files["worker-linux-amd64"])
		}, "версия"},
		{"бинарник подменён", func(f *fakeReleases) { f.files["worker-linux-amd64"] = []byte("подмена") }, "сумма"},
		{"нет файла подписи", func(f *fakeReleases) { delete(f.files, "SHA256SUMS.sig") }, ""},
		{"нет бинарника под архитектуру", func(f *fakeReleases) { delete(f.files, "worker-linux-amd64") }, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rel := newReleases(t, "новый бинарник v2")
			c.break_(rel)
			e := newUpdater(t, rel)
			err := e.u.Run(t.Context(), "")
			if err == nil {
				t.Fatal("обновление прошло")
			}
			if c.want != "" && !strings.Contains(err.Error(), c.want) {
				t.Errorf("ошибка %q не говорит про %q", err, c.want)
			}
			if content(t, e.bin) != oldBin {
				t.Error("прежний бинарник изменён")
			}
			if len(*e.events) != 0 {
				t.Errorf("до проверки подписи что-то выполнено: %v", *e.events)
			}
			if _, err := os.Stat(e.bin + ".new"); err == nil {
				t.Error("остался скачанный файл")
			}
		})
	}
}

func TestUpdateStubKeyRefuses(t *testing.T) {
	rel := newReleases(t, "новый бинарник v2")
	rel.pub = []byte("# заглушка\n")
	e := newUpdater(t, rel)
	err := e.u.Run(t.Context(), "")
	if !errors.Is(err, release.ErrNoKey) {
		t.Fatalf("с заглушкой ждали ErrNoKey, получили %v", err)
	}
	if content(t, e.bin) != oldBin {
		t.Fatal("бинарник изменён")
	}
}

func TestUpdateBackupFailureStops(t *testing.T) {
	rel := newReleases(t, "новый бинарник v2")
	e := newUpdater(t, rel)
	e.u.Backup = func(context.Context) (string, error) { return "", errors.New("диск полон") }
	if err := e.u.Run(t.Context(), ""); err == nil || !strings.Contains(err.Error(), "диск полон") {
		t.Fatalf("ошибка бэкапа потеряна: %v", err)
	}
	if content(t, e.bin) != oldBin || len(*e.events) != 0 {
		t.Fatal("без бэкапа обновление пошло дальше")
	}
}

func TestUpdateAlreadyCurrent(t *testing.T) {
	rel := newReleases(t, oldBin)
	e := newUpdater(t, rel)
	if err := e.u.Run(t.Context(), ""); err != nil {
		t.Fatal(err)
	}
	if len(*e.events) != 0 {
		t.Errorf("та же версия, а шаги выполнены: %v", *e.events)
	}
	if !strings.Contains(strings.ToLower(e.out.String()), "уже") {
		t.Errorf("вывод: %q", e.out.String())
	}
}

func TestUpdateArm64(t *testing.T) {
	rel := newReleases(t, "amd64 версия")
	rel.publish("worker-linux-arm64", []byte("arm64 версия"))
	e := newUpdater(t, rel)
	e.u.Arch = "arm64"
	if err := e.u.Run(t.Context(), ""); err != nil {
		t.Fatal(err)
	}
	if content(t, e.bin) != "arm64 версия" {
		t.Fatal("взят бинарник не той архитектуры")
	}
	e2 := newUpdater(t, rel)
	e2.u.Arch = "riscv64"
	if err := e2.u.Run(t.Context(), ""); err == nil {
		t.Fatal("неизвестная архитектура принята")
	}
}

func TestUpdateCommandArgs(t *testing.T) {
	for _, args := range [][]string{{"--version"}, {"v1.2.3"}, {"--version", "1.2.3"}, {"--version", "v1.2.3", "x"}, {"--force"}} {
		if err := updateCommand(Env{}, args, &bytes.Buffer{}); err == nil {
			t.Errorf("аргументы %v приняты", args)
		}
	}
}

// Новая версия не стартует: через проверку возвращается прежний бинарник, служба перезапущена, причина в выводе.
func TestUpdateRollsBackWhenNotHealthy(t *testing.T) {
	rel := newReleases(t, "сломанная версия")
	e := newUpdater(t, rel)
	e.u.Health = func(context.Context) error {
		*e.events = append(*e.events, "health")
		if content(t, e.bin) == "сломанная версия" {
			return errors.New("/healthz не ответил за 60 секунд")
		}
		return nil
	}
	err := e.u.Run(t.Context(), "")
	if err == nil {
		t.Fatal("обновление, после которого бот не поднялся, не считается ошибкой")
	}
	for _, want := range []string{"/healthz не ответил", "прежняя версия возвращена"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("в ошибке нет %q: %v", want, err)
		}
	}
	if content(t, e.bin) != oldBin {
		t.Fatal("прежний бинарник не возвращён")
	}
	if got := strings.Join(*e.events, ","); got != "backup,restart,health,restart,health" {
		t.Errorf("порядок шагов %s", got)
	}
	for _, leftover := range []string{e.bin + ".new", e.bin + ".old"} {
		if _, err := os.Stat(leftover); err == nil {
			t.Errorf("остался %s", leftover)
		}
	}
	if st, _ := os.Stat(e.bin); st.Mode().Perm() != 0o755 {
		t.Errorf("права после отката %v", st.Mode())
	}
}

func TestUpdateRollbackAlsoFails(t *testing.T) {
	rel := newReleases(t, "сломанная версия")
	e := newUpdater(t, rel)
	e.u.Health = func(context.Context) error { return errors.New("нет ответа") }
	err := e.u.Run(t.Context(), "")
	if err == nil || !strings.Contains(err.Error(), "journalctl") {
		t.Fatalf("при неудачном откате нужна подсказка про журнал: %v", err)
	}
	if content(t, e.bin) != oldBin {
		t.Fatal("прежний бинарник не возвращён")
	}
}

func TestUpdateRollsBackWhenRestartFails(t *testing.T) {
	rel := newReleases(t, "новая версия")
	e := newUpdater(t, rel)
	calls := 0
	e.u.Restart = func(context.Context) error {
		calls++
		if calls == 1 {
			return errors.New("unit failed")
		}
		return nil
	}
	e.u.Health = func(context.Context) error { return nil }
	err := e.u.Run(t.Context(), "")
	if err == nil || !strings.Contains(err.Error(), "unit failed") {
		t.Fatalf("причина потеряна: %v", err)
	}
	if content(t, e.bin) != oldBin || calls != 2 {
		t.Fatalf("откат не сделан: бинарник %q, перезапусков %d", content(t, e.bin), calls)
	}
}

func TestUpdateHealthyKeepsNew(t *testing.T) {
	rel := newReleases(t, "новая версия")
	e := newUpdater(t, rel)
	e.u.Health = func(context.Context) error { return nil }
	if err := e.u.Run(t.Context(), ""); err != nil {
		t.Fatal(err)
	}
	if content(t, e.bin) != "новая версия" {
		t.Fatal("рабочая версия откатилась")
	}
}

func TestWaitHealthy(t *testing.T) {
	var n int
	ok := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n++
		if n < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
	}))
	defer ok.Close()
	if err := waitHealthy(t.Context(), ok.URL, 5*time.Second, time.Millisecond); err != nil {
		t.Fatalf("поднявшийся бот не признан: %v", err)
	}

	down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(500) }))
	defer down.Close()
	start := time.Now()
	if err := waitHealthy(t.Context(), down.URL, 50*time.Millisecond, 5*time.Millisecond); err == nil {
		t.Fatal("упавший бот признан живым")
	}
	if time.Since(start) > 2*time.Second {
		t.Fatal("ждали дольше срока")
	}

	// Один ответ 200 и сразу падение: не считается, нужно два подряд.
	var m int
	flap := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		m++
		if m != 1 {
			w.WriteHeader(500)
		}
	}))
	defer flap.Close()
	if err := waitHealthy(t.Context(), flap.URL, 80*time.Millisecond, 5*time.Millisecond); err == nil {
		t.Fatal("мигающий бот признан живым по одному ответу")
	}
}

func TestHealthURL(t *testing.T) {
	for in, want := range map[string]string{
		"":               "http://127.0.0.1:8080/healthz",
		":9090":          "http://127.0.0.1:9090/healthz",
		"127.0.0.1:8081": "http://127.0.0.1:8081/healthz",
		"0.0.0.0:80":     "http://127.0.0.1:80/healthz",
		"[::]:8080":      "http://127.0.0.1:8080/healthz",
	} {
		if got := healthURL(in); got != want {
			t.Errorf("healthURL(%q) = %q, ждали %q", in, got, want)
		}
	}
}

// Старый подписанный релиз, выложенный под новым тегом, отклоняется и при latest, и при явной версии.
func TestUpdateRefusesOldReleaseUnderNewTag(t *testing.T) {
	for _, tag := range []string{"", "v1.2.3"} {
		rel := newReleases(t, "старый релиз")
		rel.version = "v1.1.0"
		rel.publish("worker-linux-amd64", rel.files["worker-linux-amd64"])
		e := newUpdater(t, rel)
		err := e.u.Run(t.Context(), tag)
		if err == nil || !strings.Contains(err.Error(), "версия") {
			t.Fatalf("tag=%q: старый релиз под новым тегом принят: %v", tag, err)
		}
		if content(t, e.bin) != oldBin || len(*e.events) != 0 {
			t.Fatalf("tag=%q: что-то изменено", tag)
		}
	}
}

func TestUpdateDowngrade(t *testing.T) {
	rel := newReleases(t, "версия 1.2.3")
	e := newUpdater(t, rel)
	e.u.Current = "v1.5.0"
	err := e.u.Run(t.Context(), "v1.2.3")
	if err == nil || !strings.Contains(err.Error(), "allow-downgrade") {
		t.Fatalf("откат на старую версию без флага принят: %v", err)
	}
	if content(t, e.bin) != oldBin {
		t.Fatal("бинарник изменён")
	}
	e.u.AllowDowngrade = true
	if err := e.u.Run(t.Context(), "v1.2.3"); err != nil {
		t.Fatalf("с флагом понижение должно пройти: %v", err)
	}
}

func TestUpdateDevBuildSkipsDowngradeCheck(t *testing.T) {
	rel := newReleases(t, "версия 1.2.3")
	e := newUpdater(t, rel)
	e.u.Current = "dev"
	if err := e.u.Run(t.Context(), ""); err != nil {
		t.Fatal(err)
	}
}

func TestCompareVersions(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want int
	}{{"v1.2.3", "v1.2.3", 0}, {"v1.2.4", "v1.2.3", 1}, {"v1.10.0", "v1.9.9", 1}, {"v0.9.0", "v1.0.0", -1}, {"v2.0.0", "v1.99.99", 1}} {
		if got := compareVersions(c.a, c.b); got != c.want {
			t.Errorf("%s vs %s = %d, ждали %d", c.a, c.b, got, c.want)
		}
	}
}

func TestUpdateCommandAllowDowngradeArg(t *testing.T) {
	if _, _, err := parseUpdateArgs([]string{"--allow-downgrade", "--version", "v1.2.3"}); err != nil {
		t.Fatal(err)
	}
	tag, allow, err := parseUpdateArgs([]string{"--version", "v1.2.3", "--allow-downgrade"})
	if err != nil || tag != "v1.2.3" || !allow {
		t.Fatalf("%q %v %v", tag, allow, err)
	}
	if _, _, err := parseUpdateArgs([]string{"--allow-downgrade", "x"}); err == nil {
		t.Fatal("лишний аргумент принят")
	}
}

// Защита от сигналов включена на всё время замены и ожидания и снята после.
func TestUpdateGuardsWholeSwap(t *testing.T) {
	rel := newReleases(t, "сломанная версия")
	e := newUpdater(t, rel)
	e.u.Guard = func() func() {
		*e.events = append(*e.events, "guard-on")
		return func() { *e.events = append(*e.events, "guard-off") }
	}
	e.u.Health = func(context.Context) error {
		if content(t, e.bin) == "сломанная версия" {
			return errors.New("не поднялась")
		}
		return nil
	}
	_ = e.u.Run(t.Context(), "")
	if got := strings.Join(*e.events, ","); got != "backup,guard-on,restart,restart,guard-off" {
		t.Errorf("порядок шагов %s", got)
	}
}

// Проверка папки идёт до скачивания: при отказе в сеть не ходят и ничего не меняют.
func TestUpdateRefusesUnsafeBinDir(t *testing.T) {
	rel := newReleases(t, "новая версия")
	e := newUpdater(t, rel)
	e.u.CheckDir = func(string) error { return errors.New("папку пишут все") }
	err := e.u.Run(t.Context(), "")
	if err == nil || !strings.Contains(err.Error(), "папку пишут все") {
		t.Fatalf("небезопасная папка принята: %v", err)
	}
	if len(rel.hits) != 0 || content(t, e.bin) != oldBin {
		t.Fatal("после отказа что-то скачано или изменено")
	}
}

// Скачанный файл получает права 755 на открытом файле, несмотря на umask 077 процесса.
func TestDownloadMode(t *testing.T) {
	rel := newReleases(t, "новая версия")
	e := newUpdater(t, rel)
	dst := e.bin + ".dl"
	if _, err := e.u.download(t.Context(), rel.srv.URL+"/download/v1.2.3/worker-linux-amd64", dst); err != nil {
		t.Fatal(err)
	}
	if st, _ := os.Stat(dst); st.Mode().Perm() != 0o755 {
		t.Errorf("права скачанного файла %v", st.Mode())
	}
}
