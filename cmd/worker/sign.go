package main

import (
	"errors"
	"fmt"
	"io"

	"github.com/bini69-oi/worker/release"
)

// signCommand это `worker sign keygen`: делает пару ключей подписи релизов. Работает без .env, это разовое
// действие владельца на своём компьютере. Закрытый ключ печатается один раз и нигде не сохраняется.
func signCommand(args []string, out io.Writer) error {
	if len(args) != 1 || args[0] != "keygen" {
		return errors.New("есть одна команда: worker sign keygen")
	}
	priv, pub, err := release.GenerateKey()
	if err != nil {
		return err
	}
	fmt.Fprintln(out, "Закрытый ключ, кладите в секрет репозитория, больше он нигде не показывается:")
	fmt.Fprintf(out, "RELEASE_SIGNING_KEY=%s\n\n", release.EncodePrivate(priv))
	fmt.Fprintln(out, "Открытый ключ, сохраните как install/release.pub и закоммитьте:")
	fmt.Fprint(out, string(pub))
	return nil
}
