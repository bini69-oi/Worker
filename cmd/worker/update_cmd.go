package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
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

// healthTimeout это сколько ждём ответа службы после перезапуска.
const healthTimeout = 60 * time.Second

// updateCommand это `worker update [--version v1.2.3]`. Запускается от root: заменяет бинарник и перезапускает службу.
// Базу root не открывает: бэкап делает копия этого же бинарника от владельца базы.
func updateCommand(env Env, args []string, out io.Writer) error {
	tag, allowDowngrade, err := parseUpdateArgs(args)
	if err != nil {
		return err
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
		Arch: runtime.GOARCH, BinPath: exe, Out: out, Current: version, AllowDowngrade: allowDowngrade, Guard: guardSignals,
		Backup:  func(ctx context.Context) (string, error) { return ownerBackup(ctx, exe, env.DBPath) },
		Restart: restartService,
		Health: func(ctx context.Context) error {
			return waitHealthy(ctx, healthURL(env.HTTPAddr), healthTimeout, time.Second)
		},
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

// healthURL это адрес /healthz на этом сервере по HTTP_ADDR из окружения: служба слушает его же.
// Адрес без хоста или на все интерфейсы превращается в 127.0.0.1. Пусто: 127.0.0.1:8080.
func healthURL(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "http://127.0.0.1:8080/healthz"
	}
	if ip := net.ParseIP(host); host == "" || (ip != nil && ip.IsUnspecified()) {
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, port) + "/healthz"
}

// waitHealthy ждёт до timeout, пока адрес ответит 200 два раза подряд. Один ответ мало: служба с ошибкой при старте
// успевает ответить и упасть, а systemd её поднимет снова.
func waitHealthy(ctx context.Context, url string, timeout, every time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	client := &http.Client{Timeout: 3 * time.Second}
	okRow := 0
	var last error
	for {
		last = probe(ctx, client, url)
		if last == nil {
			okRow++
			if okRow >= 2 {
				return nil
			}
		} else {
			okRow = 0
		}
		select {
		case <-ctx.Done():
			if last == nil {
				last = errors.New("ответил только один раз")
			}
			return fmt.Errorf("/healthz не ответил за %s: %w", timeout, last)
		case <-time.After(every):
		}
	}
}

func probe(ctx context.Context, c *http.Client, url string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("ответ %d", resp.StatusCode)
	}
	return nil
}

// parseUpdateArgs разбирает [--version vX.Y.Z] [--allow-downgrade] в любом порядке.
func parseUpdateArgs(args []string) (tag string, allowDowngrade bool, err error) {
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--allow-downgrade":
			allowDowngrade = true
		case args[i] == "--version" && i+1 < len(args) && tag == "":
			i++
			tag = args[i]
		default:
			return "", false, errors.New("запуск: worker update [--version v1.2.3] [--allow-downgrade]")
		}
	}
	return tag, allowDowngrade, nil
}
