//go:build unix

package main

import (
	"os"
	"syscall"
)

// asOwner настраивает запуск дочернего процесса от владельца папки dir, если мы root, а папка не root.
// Не root или папка root: nil, процесс идёт от текущего пользователя.
func asOwner(dir string) *syscall.SysProcAttr {
	if os.Geteuid() != 0 {
		return nil
	}
	st, err := os.Stat(dir)
	if err != nil {
		return nil
	}
	sys, ok := st.Sys().(*syscall.Stat_t)
	if !ok || sys.Uid == 0 {
		return nil
	}
	return &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: sys.Uid, Gid: sys.Gid}}
}
