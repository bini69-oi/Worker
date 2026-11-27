// Команда sign подписывает список сумм релиза в CI: sign SHA256SUMS SHA256SUMS.sig.
// Закрытый ключ берётся из переменной RELEASE_SIGNING_KEY (секрет репозитория), в аргументы он не попадает.
package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/bini69-oi/worker/release"
)

func main() {
	if err := run(os.Args[1:], os.Getenv); err != nil {
		fmt.Fprintln(os.Stderr, "sign:", err)
		os.Exit(1)
	}
}

func run(args []string, getenv func(string) string) error {
	if len(args) != 2 {
		return errors.New("запуск: sign <файл> <файл подписи>, ключ в RELEASE_SIGNING_KEY")
	}
	priv, err := release.ParsePrivate(getenv("RELEASE_SIGNING_KEY"))
	if err != nil {
		return err
	}
	msg, err := os.ReadFile(args[0])
	if err != nil {
		return err
	}
	return os.WriteFile(args[1], release.Sign(priv, msg), 0o644)
}
