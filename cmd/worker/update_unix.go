//go:build unix

package main

import (
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
)

// asOwner настраивает запуск бэкапа. Не root: процесс идёт от текущего пользователя. Root: только от владельца папки
// с базой, иначе файлы бэкапа и базы стали бы root-овыми и служба их потеряла бы. Папка root-а или относительный
// путь базы: отказ.
func asOwner(dbPath string) (*syscall.SysProcAttr, error) {
	return ownerAttr(os.Geteuid(), dbPath, func(dir string) (uint32, uint32, error) {
		st, err := os.Stat(dir)
		if err != nil {
			return 0, 0, err
		}
		sys, ok := st.Sys().(*syscall.Stat_t)
		if !ok {
			return 0, 0, errors.New("владелец папки не определён")
		}
		return sys.Uid, sys.Gid, nil
	})
}

// ownerAttr это asOwner с подставляемой проверкой владельца, чтобы проверять правила без root.
func ownerAttr(euid int, dbPath string, owner func(dir string) (uid, gid uint32, err error)) (*syscall.SysProcAttr, error) {
	if euid != 0 {
		return nil, nil
	}
	if !filepath.IsAbs(dbPath) {
		return nil, errors.New("путь к базе (DB_PATH) относительный, а update запущен от root: бэкап создал бы файлы root в папке службы. Задайте абсолютный DB_PATH")
	}
	dir := filepath.Dir(dbPath)
	uid, gid, err := owner(dir)
	if err != nil {
		return nil, err
	}
	if uid == 0 {
		return nil, fmt.Errorf("папка базы %s принадлежит root: бэкап от root создал бы файлы root в папке службы. "+
			"Папка должна принадлежать пользователю службы (chown worker:worker)", dir)
	}
	return &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: uid, Gid: gid}}, nil
}

// guardSignals перехватывает сигналы, которые иначе убили бы процесс посреди замены бинарника: Ctrl+C, обрыв ssh,
// SIGTERM и SIGPIPE (запись в закрытый терминал). Возвращает функцию, которая возвращает обычное поведение.
func guardSignals() func() {
	ch := make(chan os.Signal, 8)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGHUP, syscall.SIGTERM, syscall.SIGPIPE)
	return func() { signal.Stop(ch) }
}
