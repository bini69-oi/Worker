// Команда worker запускает бота: читает .env, открывает базу и запускает всё в одном процессе.
//
// Служебные команды на сервере:
//
//	worker rotate            сменить мастер-ключ и перешифровать секреты
//	worker admin password    задать пароль владельца
package main

import (
	"fmt"
	"log/slog"
	"os"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))

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
			fmt.Fprintln(os.Stderr, "ошибка:", err)
			os.Exit(1)
		}
		return
	}
	log.Info("настройки прочитаны", "public_url", env.PublicURL, "db", env.DBPath)
}
