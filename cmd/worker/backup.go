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
	if len(args) != 0 {
		return errors.New("запуск: worker backup")
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
