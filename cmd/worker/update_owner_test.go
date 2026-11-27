//go:build unix

package main

import (
	"errors"
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
