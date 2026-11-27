// Команда gen-types пишет web/src/lib/types.gen.ts из структур ответов пакета api. Запуск: make types.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/bini69-oi/worker/api"
)

func main() {
	out := flag.String("o", "web/src/lib/types.gen.ts", "куда писать файл типов")
	flag.Parse()
	text, err := generate(api.Types())
	if err != nil {
		fmt.Fprintln(os.Stderr, "gen-types:", err)
		os.Exit(1)
	}
	if err := os.WriteFile(*out, []byte(text), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "gen-types:", err)
		os.Exit(1)
	}
}
