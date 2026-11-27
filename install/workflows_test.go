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

// Действия в workflow закреплены по хэшу коммита, как сказано в docs/decisions.md.
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

// Релиз собирается на тег, подписывается и пишется в релиз только с правом contents: write у одной задачи.
func TestReleaseWorkflowShape(t *testing.T) {
	b, err := os.ReadFile("../.github/workflows/release.yml")
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, want := range []string{
		"tags:", "worker-linux-amd64", "worker-linux-arm64", "SHA256SUMS", "cmd/sign",
		"RELEASE_SIGNING_KEY", `echo "version $TAG"`, "persist-credentials: false", "contents: write",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("в release.yml нет %q", want)
		}
	}
	for _, line := range strings.Split(s, "\n") {
		if strings.Contains(line, "${{") && !envLine.MatchString(strings.TrimSpace(line)) {
			t.Errorf("выражение подставлено не в env, а в команду или шаг: %q", strings.TrimSpace(line))
		}
	}
}
