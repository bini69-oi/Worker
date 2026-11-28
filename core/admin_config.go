package core

import (
	"context"
	"time"

	"github.com/bini69-oi/worker/settings"
)

// ownerResult это что действие владельца отдаёт ядру: над чем оно, подробности для журнала и то, что надо
// сделать в памяти после коммита (применить настройку или текст в кэше).
type ownerResult struct {
	Target  string
	Details map[string]string
	After   []func()
}

// ownerAction выполняет действие владельца над настройками бота. Право проверяется (отказ в журнал и тревогой),
// запись настройки или текста и запись в журнал идут одной транзакцией (asAdmin), а кэши настроек и текстов
// обновляются после коммита. Не получилось действие: нет ни записи, ни журнала, ни изменений в памяти.
func (c *Core) ownerAction(ctx context.Context, actorTg int64, action string,
	do func(tx Tx, now time.Time, res *ownerResult) error) error {
	if _, err := c.Require(ctx, actorTg, PermOwner, action); err != nil {
		return err
	}
	var res ownerResult
	err := c.asAdmin(ctx, actorTg, PermOwner, action, func(tx Tx, _ Admin, now time.Time) (string, map[string]string, error) {
		res = ownerResult{}
		if err := do(tx, now, &res); err != nil {
			return "", nil, err
		}
		return res.Target, res.Details, nil
	})
	if err != nil {
		return err
	}
	for _, f := range res.After {
		f()
	}
	return nil
}

// writeChange пишет подготовленную правку настройки в транзакцию и откладывает применение кэша на после коммита.
func (c *Core) writeChange(tx Tx, cfg ConfigAdmin, ch settings.Change, now time.Time, res *ownerResult) error {
	var err error
	if ch.Delete {
		err = tx.DeleteSetting(ch.Key)
	} else {
		err = tx.SaveSetting(ch.Key, ch.Stored, now)
	}
	res.After = append(res.After, func() { cfg.Apply(ch) })
	return err
}
