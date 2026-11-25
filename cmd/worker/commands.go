package main

import (
	"bufio"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"

	"github.com/bini69-oi/worker/core"
	"github.com/bini69-oi/worker/settings"
	"github.com/bini69-oi/worker/store"
)

// command выполняет служебную команду. Это действия только с сервера: из бота и админки их не сделать.
func command(env Env, args []string, in io.Reader, out io.Writer) error {
	switch strings.Join(args, " ") {
	case "rotate":
		return rotate(env, out)
	case "admin password":
		return ownerPassword(env, in, out)
	}
	return errors.New("неизвестная команда, есть: rotate, admin password")
}

// ownerPassword задаёт пароль владельца. Пароль просится дважды; в терминале ввод не виден.
func ownerPassword(env Env, in io.Reader, out io.Writer) error {
	read := lineReader(in, out)
	pw, err := read("Новый пароль владельца (не короче 10 символов): ")
	if err != nil {
		return err
	}
	again, err := read("Ещё раз: ")
	if err != nil {
		return err
	}
	if pw != again {
		return errors.New("пароли не совпали")
	}
	db, err := store.Open(env.DBPath)
	if err != nil {
		return err
	}
	defer db.Close()
	c := core.New(core.Deps{Store: store.NewCoreStore(db), OwnerID: env.OwnerID})
	if err := c.SetOwnerPassword(context.Background(), pw); err != nil {
		return err
	}
	fmt.Fprintln(out, "Пароль владельца задан. Все старые сессии закрыты.")
	return nil
}

// lineReader читает строку: из терминала без показа на экране, иначе обычной строкой.
func lineReader(in io.Reader, out io.Writer) func(prompt string) (string, error) {
	sc := bufio.NewScanner(in)
	return func(prompt string) (string, error) {
		fmt.Fprint(out, prompt)
		if f, ok := in.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
			b, err := term.ReadPassword(int(f.Fd()))
			fmt.Fprintln(out)
			return string(b), err
		}
		if !sc.Scan() {
			return "", errors.New("пароль не введён")
		}
		return strings.TrimRight(sc.Text(), "\r"), nil
	}
}

// rotate меняет мастер-ключ: перешифровывает все секреты новым ключом одной транзакцией и печатает новый ключ.
// Процесс бота на это время надо остановить, а после команды вписать новый ключ в .env.
//
// Новый ключ сначала записывается в файл рядом с базой (права 600) и только потом идёт перешифровка:
// если вывод в терминал потеряется (оборвался ssh), ключ останется в файле, а не пропадёт вместе с секретами.
func rotate(env Env, out io.Writer) error {
	db, err := store.Open(env.DBPath)
	if err != nil {
		return err
	}
	defer db.Close()
	newKey := settings.NewKey()
	line := "SECRET_KEY=" + base64.StdEncoding.EncodeToString(newKey) + "\n"
	keyFile := env.DBPath + ".newkey"
	if err := os.WriteFile(keyFile, []byte(line), 0o600); err != nil {
		return fmt.Errorf("новый ключ не сохранён в файл, база не тронута: %w", err)
	}
	err = store.NewSettingsStore(db).Rotate(func(rows map[string]string) (map[string]string, error) {
		return settings.Reencrypt(rows, env.SecretKey, newKey)
	})
	if err != nil {
		_ = os.Remove(keyFile)
		return fmt.Errorf("секреты не перешифрованы, база на старом ключе: %w", err)
	}
	fmt.Fprintf(out, "Готово. Новый ключ лежит в %s, впишите его в .env вместо старого и удалите файл:\n%s", keyFile, line)
	return nil
}
