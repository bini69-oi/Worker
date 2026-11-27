package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/bini69-oi/worker/install"
)

// serviceName это имя службы systemd, которую ставит install.sh.
const serviceName = "worker"

// updateCommand это `worker update [--version v1.2.3]`. Запускается от root: заменяет бинарник и перезапускает службу.
// Базу root не открывает: бэкап делает копия этого же бинарника от владельца базы.
func updateCommand(env Env, args []string, out io.Writer) error {
	tag := ""
	switch {
	case len(args) == 0:
	case len(args) == 2 && args[0] == "--version":
		tag = args[1]
	default:
		return errors.New("запуск: worker update [--version v1.2.3]")
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if exe, err = filepath.EvalSymlinks(exe); err != nil {
		return err
	}
	u := &updater{
		BaseURL: releasesURL, Client: &http.Client{Timeout: 5 * time.Minute}, PubKey: install.ReleasePub,
		Arch: runtime.GOARCH, BinPath: exe, Out: out,
		Backup:  func(ctx context.Context) (string, error) { return ownerBackup(ctx, exe, env.DBPath) },
		Restart: restartService,
	}
	fmt.Fprintf(out, "Сейчас стоит %s.\n", version)
	return u.Run(context.Background(), tag)
}

// ownerBackup запускает `<бинарник> backup` от пользователя, которому принадлежит папка с базой, и возвращает
// путь к файлу бэкапа. Так файлы бэкапа и базы не меняют владельца.
func ownerBackup(ctx context.Context, exe, dbPath string) (string, error) {
	cmd := exec.CommandContext(ctx, exe, "backup")
	cmd.SysProcAttr = asOwner(filepath.Dir(dbPath))
	b, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%w: %s", err, strings.TrimSpace(string(b)))
	}
	return strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(string(b)), "Бэкап готов:")), nil
}

func restartService(ctx context.Context) error {
	if b, err := exec.CommandContext(ctx, "systemctl", "restart", serviceName).CombinedOutput(); err != nil {
		return fmt.Errorf("systemctl restart %s: %w: %s", serviceName, err, strings.TrimSpace(string(b)))
	}
	return nil
}
