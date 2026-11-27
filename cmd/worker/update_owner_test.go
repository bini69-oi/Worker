//go:build unix

package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestOwnerAttr(t *testing.T) {
	owner := func(uid, gid uint32) func(string) (uint32, uint32, error) {
		return func(string) (uint32, uint32, error) { return uid, gid, nil }
	}
	failing := func(string) (uint32, uint32, error) { return 0, 0, errors.New("нет папки") }

	// Не root: бэкап идёт от текущего пользователя.
	if a, err := ownerAttr(1000, "worker.db", owner(0, 0)); a != nil || err != nil {
		t.Errorf("не root: %v %v", a, err)
	}
	// Root, папка пользователя службы: от владельца папки.
	a, err := ownerAttr(0, "/var/lib/worker/worker.db", owner(998, 997))
	if err != nil || a == nil || a.Credential.Uid != 998 || a.Credential.Gid != 997 {
		t.Errorf("root и папка worker: %+v %v", a, err)
	}
	// Root: папка root, относительный путь, нет папки: отказ, никогда не от root.
	for name, c := range map[string]struct {
		path string
		own  func(string) (uint32, uint32, error)
	}{
		"папка root":         {"/var/lib/worker/worker.db", owner(0, 0)},
		"относительный путь": {"worker.db", owner(998, 997)},
		"путь с точками":     {"../worker.db", owner(998, 997)},
		"папки нет":          {"/нет/worker.db", failing},
	} {
		if a, err := ownerAttr(0, c.path, c.own); err == nil || a != nil {
			t.Errorf("%s: должен быть отказ, получили %+v %v", name, a, err)
		}
	}
}

func TestBinDirProblem(t *testing.T) {
	cases := []struct {
		name string
		mode os.FileMode
		uid  uint32
		bad  bool
	}{
		{"root 755", 0o755, 0, false},
		{"root 750", 0o750, 0, false},
		{"root 775 пишет группа", 0o775, 0, true},
		{"root 757 пишут все", 0o757, 0, true},
		{"root 777", 0o777, 0, true},
		{"worker 755", 0o755, 998, true},
		{"пользователь 700", 0o700, 1000, true},
	}
	for _, c := range cases {
		err := binDirProblem("/usr/local/bin", c.mode, c.uid)
		if (err != nil) != c.bad {
			t.Errorf("%s: err=%v", c.name, err)
		}
	}
}

func TestCheckBinDirRealDir(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o777); err != nil {
		t.Fatal(err)
	}
	if err := checkBinDir(dir); err == nil {
		t.Error("папка с правами 777 принята")
	}
	if err := checkBinDir(filepath.Join(dir, "нет")); err == nil {
		t.Error("несуществующая папка принята")
	}
}
