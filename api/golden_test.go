package api

import (
	"bytes"
	"encoding/json"
	"flag"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/bini69-oi/worker/core"
)

var update = flag.Bool("update", false, "переписать эталоны ответов API в testdata")

// golden сравнивает тело ответа с эталоном testdata/<name>.json. Эталон это тот же JSON, что отдаёт сервер,
// с отступами для диффа. Тесты фронта берут эти файлы как ответы сервера. С флагом -update эталон переписывается:
// запустите go test ./api -update и посмотрите дифф глазами.
func golden(t *testing.T, name string, body []byte) {
	t.Helper()
	var pretty bytes.Buffer
	if err := json.Indent(&pretty, bytes.TrimSpace(body), "", "  "); err != nil {
		t.Fatalf("ответ для эталона %s не JSON: %v\n%s", name, err, body)
	}
	pretty.WriteByte('\n')
	path := filepath.Join("testdata", name+".json")
	if *update {
		if err := os.WriteFile(path, pretty.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("нет эталона %s: запустите go test ./api -update и проверьте дифф", path)
	}
	if !bytes.Equal(want, pretty.Bytes()) {
		t.Fatalf("ответ %s не совпал с эталоном.\nждали:\n%s\nполучили:\n%s", name, want, pretty.Bytes())
	}
}

func TestGoldenErrorBody(t *testing.T) {
	s := newTestServer(t)
	s.Handle("GET /api/x", func(w http.ResponseWriter, r *http.Request) (any, error) { return nil, core.ErrNotFound })
	rec := do(s, "GET", "/api/x", "")
	golden(t, "error_not_found", rec.Body.Bytes())
}

// Каждый эталон в testdata должен быть верным JSON: фронт читает их как ответы сервера.
func TestGoldenFilesAreJSON(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("testdata", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if !json.Valid(raw) {
			t.Errorf("%s не JSON", f)
		}
	}
}
