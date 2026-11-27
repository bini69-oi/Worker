// Команда worker запускает бота: читает .env, открывает базу и запускает всё в одном процессе.
//
// Служебные команды на сервере:
//
//	worker rotate            сменить мастер-ключ и перешифровать секреты
//	worker admin password    задать пароль владельца
//	worker backup            зашифрованный бэкап базы в папку backups рядом с базой
//	worker backup --check F  проверить бэкап: открывается ключом, база целая
//	worker restore F         восстановить базу из бэкапа (служба остановлена)
//	worker update [--version v1.2.3]  обновить бинарник из подписанного релиза (от root)
//	worker version           версия сборки (без .env)
//	worker sign keygen       создать пару ключей подписи релизов (без .env)
package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/bini69-oi/worker/redact"
)

func main() {
	privateFiles()
	base := slog.NewJSONHandler(os.Stdout, nil)
	log := slog.New(base)

	if len(os.Args) == 2 && os.Args[1] == "version" {
		fmt.Println(version)
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "sign" {
		if err := signCommand(os.Args[2:], os.Stdout); err != nil {
			fmt.Fprintln(os.Stderr, "ошибка:", err)
			os.Exit(1)
		}
		return
	}

	env, warns, err := loadEnv(envPath(os.Getenv, fileExists, os.Geteuid()), os.Getenv)
	for _, w := range warns {
		log.Warn(w)
	}
	if err != nil {
		log.Error("не удалось прочитать настройки", "err", err)
		os.Exit(1)
	}

	if len(os.Args) > 1 {
		if err := command(env, os.Args[1:], os.Stdin, os.Stdout); err != nil {
			secrets := &redact.Set{}
			secrets.Add(env.BotToken, env.PanelToken, base64.StdEncoding.EncodeToString(env.SecretKey))
			fmt.Fprintln(os.Stderr, "ошибка:", secrets.String(err.Error()))
			os.Exit(1)
		}
		return
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, env, base); err != nil {
		log.Error("worker упал", "err", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, env Env, base slog.Handler) error {
	unlock, err := lockDB(env.DBPath)
	if err != nil {
		return err
	}
	defer unlock()
	msg, err := checkKeyFile(env)
	if err != nil {
		return err
	}
	if msg != "" {
		slog.New(base).Info(msg)
	}
	a, err := build(env, base)
	if err != nil {
		return err
	}
	ln, err := net.Listen("tcp", listenAddr(env, a.cfg.String("http.addr")))
	if err != nil {
		a.db.Close()
		return err
	}
	return a.serve(ctx, ln)
}
