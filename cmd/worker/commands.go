package main

import (
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/bini69-oi/worker/settings"
	"github.com/bini69-oi/worker/store"
)

// command выполняет служебную команду. Это действия только с сервера: из бота и админки их не сделать.
func command(env Env, args []string, in io.Reader, out io.Writer) error {
	switch strings.Join(args, " ") {
	case "rotate":
		return rotate(env, out)
	}
	return errors.New("неизвестная команда, есть: rotate")
}

// rotate меняет мастер-ключ: перешифровывает все секреты новым ключом одной транзакцией и печатает новый ключ.
// Процесс бота на это время надо остановить, а после команды вписать новый ключ в .env.
func rotate(env Env, out io.Writer) error {
	db, err := store.Open(env.DBPath)
	if err != nil {
		return err
	}
	defer db.Close()
	newKey := settings.NewKey()
	err = store.NewSettingsStore(db).Rotate(func(rows map[string]string) (map[string]string, error) {
		return settings.Reencrypt(rows, env.SecretKey, newKey)
	})
	if err != nil {
		return fmt.Errorf("секреты не перешифрованы, база на старом ключе: %w", err)
	}
	fmt.Fprintf(out, "Готово. Новый ключ, впишите его в .env вместо старого:\nSECRET_KEY=%s\n",
		base64.StdEncoding.EncodeToString(newKey))
	return nil
}
