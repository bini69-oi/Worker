// Команда worker запускает бота: читает .env, открывает базу и запускает всё в одном процессе.
package main

import (
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
	log.Info("настройки прочитаны", "public_url", env.PublicURL, "db", env.DBPath)
}
