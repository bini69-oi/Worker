//go:build unix

package main

import (
	"errors"
	"os"
	"syscall"
)

// errLocked значит, что базу уже держит другой процесс worker.
var errLocked = errors.New("база занята другим процессом worker: остановите бота и повторите")

// lockDB берёт замок на базу: файл рядом с ней с блокировкой flock. Бот держит замок всё время работы,
// а rotate без замка не начнёт, поэтому смена ключа при работающем боте невозможна.
// Замок снимается сам, если процесс упал.
func lockDB(dbPath string) (func(), error) {
	f, err := os.OpenFile(dbPath+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, errLocked
		}
		return nil, err
	}
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		f.Close()
	}, nil
}

// privateFiles делает новые файлы процесса закрытыми для других пользователей сервера.
func privateFiles() { syscall.Umask(0o077) }
