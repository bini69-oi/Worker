package texts

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// keyRe находит в коде страницы ключи текстов: 'app.something' или 'app.reason_' с продолжением в коде.
var keyRe = regexp.MustCompile(`['"` + "`" + `]((?:app|err)\.[a-z0-9_]+)(['"` + "`" + `])`)

// TestWebKeysExist проверяет, что каждый ключ, который зовёт страница, есть среди текстов, и что у него есть английский.
// Иначе абонент увидит на экране сам ключ.
func TestWebKeysExist(t *testing.T) {
	root := filepath.Join("..", "web", "src")
	if _, err := os.Stat(root); err != nil {
		t.Skip("нет web/src")
	}
	have := map[string]Def{}
	for _, d := range defs {
		have[d.Key] = d
	}
	used := map[string]string{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		name := d.Name()
		if strings.Contains(path, string(filepath.Separator)+"testing"+string(filepath.Separator)) ||
			strings.HasSuffix(name, ".test.ts") || strings.HasSuffix(name, ".gen.ts") ||
			!(strings.HasSuffix(name, ".ts") || strings.HasSuffix(name, ".svelte")) {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, m := range keyRe.FindAllStringSubmatch(string(raw), -1) {
			used[m[1]] = path
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for key, path := range used {
		if strings.HasSuffix(key, "_") {
			continue // начало ключа, остальное собирается в коде: проверяется отдельно ниже
		}
		d, ok := have[key]
		if !ok {
			t.Errorf("ключ %s из %s не описан в texts", key, path)
			continue
		}
		if d.En == "" {
			t.Errorf("у ключа %s нет английского текста", key)
		}
	}
}

// TestAppKeysAreRussianAndEnglish: у каждого app.* есть оба языка и ни одного длинного тире.
func TestAppKeysAreRussianAndEnglish(t *testing.T) {
	for _, d := range appDefs {
		if d.Default == "" || d.En == "" {
			t.Errorf("%s: нужны русский и английский тексты", d.Key)
		}
		if strings.ContainsAny(d.Default+d.En, "—–") {
			t.Errorf("%s: в тексте длинное тире", d.Key)
		}
	}
}
