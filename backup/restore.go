package backup

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"time"

	"github.com/bini69-oi/worker/store"
)

// suffixes это файлы, из которых состоит база SQLite в режиме WAL.
var suffixes = []string{"", "-wal", "-shm"}

// Restore подменяет базу dbPath расшифрованным бэкапом. Вызывающий держит замок базы (служба остановлена).
// Порядок: расшифровка рядом с базой, проверка целостности, текущая база откладывается в
// <база>.before-restore-<время>, бэкап встаёт на её место, готовая база проверяется ещё раз.
// Любая неудача возвращает всё как было. Возвращает путь к отложенной копии ("", если базы не было).
func Restore(dbPath, backupPath string, secret []byte, now time.Time) (string, error) {
	tmp := dbPath + ".restore-tmp"
	_ = os.Remove(tmp)
	if err := Decrypt(backupPath, secret, tmp); err != nil {
		return "", err
	}
	defer os.Remove(tmp)
	if err := store.CheckFile(tmp); err != nil {
		return "", fmt.Errorf("бэкап расшифрован, но база в нём негодная: %w", err)
	}

	saved := dbPath + ".before-restore-" + now.UTC().Format(nameLayout)
	had, err := moveSet(dbPath, saved)
	if err != nil {
		return "", fmt.Errorf("текущая база не отложена, ничего не изменено: %w", err)
	}
	rollback := func() {
		for _, s := range suffixes {
			_ = os.Remove(dbPath + s)
		}
		if had {
			_, _ = moveSet(saved, dbPath)
		}
	}
	if err := os.Rename(tmp, dbPath); err != nil {
		rollback()
		return "", fmt.Errorf("база не подменена, прежняя на месте: %w", err)
	}
	if err := store.CheckFile(dbPath); err != nil {
		rollback()
		return "", fmt.Errorf("после подмены база не прошла проверку, прежняя возвращена: %w", err)
	}
	if !had {
		return "", nil
	}
	return saved, nil
}

// moveSet переименовывает файл базы вместе с -wal и -shm. Сообщает, был ли основной файл.
// При ошибке уже перенесённое возвращает назад.
func moveSet(from, to string) (bool, error) {
	var moved []string
	had := false
	for _, s := range suffixes {
		err := os.Rename(from+s, to+s)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			for _, m := range moved {
				_ = os.Rename(to+m, from+m)
			}
			return false, err
		}
		moved = append(moved, s)
		had = had || s == ""
	}
	return had, nil
}
