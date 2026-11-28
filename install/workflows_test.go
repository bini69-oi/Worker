package install

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// envLine это присваивание переменной окружения шага: туда выражения подставлять безопасно.
var envLine = regexp.MustCompile(`^[A-Z_]+: \$\{\{ [^}]+ \}\}$`)

var pinned = regexp.MustCompile(`^[\w.-]+/[\w./-]+@[0-9a-f]{40}( #.*)?$`)

// Действия в workflow закреплены по хэшу коммита: тег можно перевесить, коммит нельзя.
func TestWorkflowActionsPinned(t *testing.T) {
	files, _ := filepath.Glob("../.github/workflows/*.yml")
	if len(files) == 0 {
		t.Fatal("workflow не найдены")
	}
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, line := range strings.Split(string(b), "\n") {
			_, ref, ok := strings.Cut(strings.TrimSpace(line), "uses:")
			if ok && !pinned.MatchString(strings.TrimSpace(ref)) {
				t.Errorf("%s: действие не закреплено по хэшу: %q", f, strings.TrimSpace(ref))
			}
		}
	}
}

// jobSection возвращает текст задачи name из workflow.
func jobSection(t *testing.T, s, name string) string {
	t.Helper()
	start := strings.Index(s, "\n  "+name+":\n")
	if start < 0 {
		t.Fatalf("в release.yml нет задачи %s", name)
	}
	rest := s[start+1:]
	if end := regexp.MustCompile(`\n  [a-z]+:\n`).FindStringIndex(rest[len(name)+4:]); end != nil {
		return rest[:len(name)+4+end[0]]
	}
	return rest
}

// Релиз: сборка без секрета, подпись в отдельной задаче с окружением, публикация третьей с правом записи.
func TestReleaseWorkflowShape(t *testing.T) {
	b, err := os.ReadFile("../.github/workflows/release.yml")
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, want := range []string{
		"tags:", "worker-linux-amd64", "worker-linux-arm64", "SHA256SUMS", `echo "version $TAG"`, "persist-credentials: false",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("в release.yml нет %q", want)
		}
	}
	build, sign, publish := jobSection(t, s, "build"), jobSection(t, s, "sign"), jobSection(t, s, "publish")

	if strings.Count(s, "secrets.RELEASE_SIGNING_KEY") != 1 || !strings.Contains(sign, "secrets.RELEASE_SIGNING_KEY") {
		t.Error("секрет подписи должен упоминаться только в задаче sign")
	}
	if !strings.Contains(sign, "needs: build") || !strings.Contains(sign, "environment: release") {
		t.Error("sign: нужны needs: build и environment: release")
	}
	if strings.Contains(sign, "npm") || strings.Contains(sign, "setup-node") {
		t.Error("в задаче sign не должно быть npm")
	}
	if !strings.Contains(sign, "go build -o \"$RUNNER_TEMP/sign\" ./cmd/sign") || strings.Contains(sign, "go run") {
		t.Error("sign: инструмент подписи собирается в чистой проверке, а не запускается через go run")
	}
	for name, section := range map[string]string{"build": build, "sign": sign} {
		if !strings.Contains(section, "cache: false") {
			t.Errorf("%s: у setup-go должен быть cache: false", name)
		}
	}
	if !strings.Contains(build, "npm ci --ignore-scripts") || strings.Contains(build, "npm ci\n") {
		t.Error("build: npm ci только с --ignore-scripts")
	}
	if !strings.Contains(build, "merge-base --is-ancestor") {
		t.Error("build: нет проверки, что тег стоит на коммите из main")
	}
	if strings.Contains(build, "contents: write") || strings.Contains(sign, "contents: write") || !strings.Contains(publish, "contents: write") {
		t.Error("право contents: write только у задачи publish")
	}
	if strings.Count(s, "contents: write") != 1 {
		t.Error("contents: write должно быть ровно в одном месте")
	}
	for _, line := range strings.Split(s, "\n") {
		if strings.Contains(line, "${{") && !envLine.MatchString(strings.TrimSpace(line)) {
			t.Errorf("выражение подставлено не в env, а в команду или шаг: %q", strings.TrimSpace(line))
		}
	}
}
