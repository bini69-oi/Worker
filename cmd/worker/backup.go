package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"time"

	"github.com/bini69-oi/worker/backup"
	"github.com/bini69-oi/worker/settings"
	"github.com/bini69-oi/worker/store"
)

// backupDir это папка бэкапов: рядом с базой.
func backupDir(dbPath string) string { return filepath.Join(filepath.Dir(dbPath), "backups") }

// backupCommand это `worker backup`: зашифрованная копия базы. Бота останавливать не нужно.
func backupCommand(env Env, args []string, out io.Writer) error {
	if len(args) == 2 && args[0] == "--check" {
		if err := backup.Check(args[1], env.SecretKey); err != nil {
			return err
		}
		fmt.Fprintln(out, "Бэкап открывается ключом и база в нём в порядке.")
		return nil
	}
	if len(args) != 0 {
		return errors.New("запуск: worker backup или worker backup --check <файл>")
	}
	db, err := store.Open(env.DBPath)
	if err != nil {
		return err
	}
	defer db.Close()
	cfg, err := settings.New(store.NewSettingsStore(db), env.SecretKey, time.Now)
	if err != nil {
		return err
	}
	svc := &backup.Service{
		DB: db, Secret: env.SecretKey, Dir: backupDir(env.DBPath),
		Keep: func() int { return int(cfg.Int("backup.keep")) }, Now: time.Now,
	}
	path, err := svc.Create(context.Background())
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "Бэкап готов: %s\n", path)
	return nil
}

// restoreCommand это `worker restore <файл>`: база заменяется бэкапом. Берёт замок базы, поэтому
// при работающем боте отказывает: остановите службу (systemctl stop worker), восстановите, запустите.
func restoreCommand(env Env, args []string, out io.Writer) error {
	if len(args) != 1 {
		return errors.New("запуск: worker restore <файл бэкапа>")
	}
	unlock, err := lockDB(env.DBPath)
	if err != nil {
		return err
	}
	defer unlock()
	saved, err := backup.Restore(env.DBPath, args[0], env.SecretKey, time.Now())
	if err != nil {
		return err
	}
	if saved != "" {
		fmt.Fprintf(out, "База восстановлена. Прежняя отложена в %s: когда убедитесь, что всё в порядке, удалите её.\n", saved)
		return nil
	}
	fmt.Fprintln(out, "База восстановлена.")
	return nil
}
