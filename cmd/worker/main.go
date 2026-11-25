// Команда worker запускает бота: читает .env, открывает базу и запускает всё в одном процессе.
//
// Служебные команды на сервере:
//
//	worker rotate            сменить мастер-ключ и перешифровать секреты
//	worker admin password    задать пароль владельца
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
	base := slog.NewJSONHandler(os.Stdout, nil)
	log := slog.New(base)

	env, warns, err := loadEnv(".env", os.Getenv)
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
	a, err := build(env, base)
	if err != nil {
		return err
	}
	ln, err := net.Listen("tcp", a.cfg.String("http.addr"))
	if err != nil {
		a.db.Close()
		return err
	}
	return a.serve(ctx, ln)
}
