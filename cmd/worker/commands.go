package main

import (
	"bufio"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strings"

	"golang.org/x/term"

	"github.com/bini69-oi/worker/core"
	"github.com/bini69-oi/worker/settings"
	"github.com/bini69-oi/worker/store"
)

// command выполняет служебную команду. Это действия только с сервера: из бота и админки их не сделать.
func command(env Env, args []string, in io.Reader, out io.Writer) error {
	if len(args) > 0 && args[0] == "backup" {
		return backupCommand(env, args[1:], out)
	}
	switch strings.Join(args, " ") {
	case "rotate":
		return rotate(env, out)
	case "admin password":
		return ownerPassword(env, in, out)
	case "import panel":
		return importPanel(env, false, out)
	case "import panel --apply":
		return importPanel(env, true, out)
	}
	return errors.New("неизвестная команда, есть: rotate, admin password, import panel [--apply], backup")
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
// Бот на это время должен быть остановлен: rotate берёт замок базы и при работающем боте отказывает.
// После команды новый ключ вписывается в .env, а файл с ним бот удалит сам при следующем старте.
//
// Новый ключ сначала записывается в файл рядом с базой (права 600) и только потом идёт перешифровка:
// если вывод в терминал потеряется (оборвался ssh), ключ останется в файле. Файл создаётся только новым:
// если он уже есть, прошлая смена ключа не закончена, и затереть её ключ нельзя.
func rotate(env Env, out io.Writer) error {
	unlock, err := lockDB(env.DBPath)
	if err != nil {
		return err
	}
	defer unlock()
	path := keyFile(env.DBPath)
	newKey := settings.NewKey()
	line := "SECRET_KEY=" + base64.StdEncoding.EncodeToString(newKey) + "\n"
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, fs.ErrExist) {
		return fmt.Errorf("прошлая смена ключа не закончена: впишите SECRET_KEY из %s в .env и запустите бота, "+
			"он удалит файл сам", path)
	}
	if err != nil {
		return fmt.Errorf("новый ключ не сохранён в файл, база не тронута: %w", err)
	}
	if _, err := f.WriteString(line); err != nil {
		f.Close()
		_ = os.Remove(path)
		return fmt.Errorf("новый ключ не сохранён в файл, база не тронута: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(path)
		return err
	}
	db, err := store.Open(env.DBPath)
	if err != nil {
		_ = os.Remove(path)
		return err
	}
	defer db.Close()
	err = store.NewSettingsStore(db).Rotate(func(rows map[string]string) (map[string]string, error) {
		return settings.Reencrypt(rows, env.SecretKey, newKey)
	})
	if err != nil {
		// Файл создан этим запуском, а транзакция откатилась: база на прежнем ключе, новый ключ не нужен.
		_ = os.Remove(path)
		return fmt.Errorf("секреты не перешифрованы, база на прежнем ключе: %w", err)
	}
	fmt.Fprintf(out, "Готово. Новый ключ лежит в %s, впишите его в .env вместо старого и запустите бота:\n%s", path, line)
	return nil
}
