package main

import (
	"cmp"
	"context"
	"fmt"
	"io"
	"maps"
	"os"
	"slices"
	"time"

	"github.com/bini69-oi/worker/core"
	"github.com/bini69-oi/worker/panel"
	"github.com/bini69-oi/worker/settings"
	"github.com/bini69-oi/worker/store"
)

// importPanel переносит пользователей панели в абонентов. Без apply только считает и печатает отчёт.
// Бот на это время остановлен: команда берёт замок базы, чтобы сверка не шла посреди переноса.
func importPanel(env Env, apply bool, out io.Writer) error {
	unlock, err := lockDB(env.DBPath)
	if err != nil {
		return err
	}
	defer unlock()
	// Опечатка в пути не должна создать пустую базу: импорт идёт только в уже работавшую.
	if _, err := os.Stat(env.DBPath); err != nil {
		return fmt.Errorf("база не найдена: %w", err)
	}
	db, err := store.Open(env.DBPath)
	if err != nil {
		return err
	}
	defer db.Close()
	cfg, err := settings.New(store.NewSettingsStore(db), env.SecretKey, time.Now)
	if err != nil {
		return err
	}
	c := core.New(core.Deps{Store: store.NewCoreStore(db), Config: cfg, OwnerID: env.OwnerID,
		Panel: panel.CoreAdapter{Client: panel.New(env.PanelURL, env.PanelToken, nil)}})
	rep, err := c.ImportPanel(context.Background(), env.OwnerID, apply)
	if err != nil {
		return err
	}
	printImport(out, rep)
	return nil
}

func printImport(out io.Writer, rep core.ImportReport) {
	verb := "Будет заведено"
	if rep.Applied {
		verb = "Заведено"
	}
	fmt.Fprintf(out, "%s подписок: %d, из них новых абонентов: %d\n", verb, rep.Subs, rep.NewUsers)
	if len(rep.Reasons) > 0 {
		fmt.Fprintln(out, "Пропущено:")
		// Сначала частые причины, при равенстве по алфавиту, чтобы отчёт не прыгал от запуска к запуску.
		reasons := slices.SortedFunc(maps.Keys(rep.Reasons), func(a, b string) int {
			return cmp.Or(cmp.Compare(rep.Reasons[b], rep.Reasons[a]), cmp.Compare(a, b))
		})
		for _, r := range reasons {
			fmt.Fprintf(out, "  %s: %d\n", r, rep.Reasons[r])
		}
		fmt.Fprintln(out, "Примеры:")
		for _, s := range rep.Skipped {
			fmt.Fprintf(out, "  %s: %s\n", cmp.Or(s.Username, "без имени"), s.Reason)
		}
	}
	if !rep.Applied {
		fmt.Fprintln(out, "Это пробный прогон, база не тронута. Записать: worker import panel --apply")
		return
	}
	fmt.Fprintln(out, "Сроки, поправленные в панели руками после переноса, сверка вернёт к базе.")
}
