package core

import (
	"context"
	"time"
)

// ownerAction выполняет действие владельца над настройками бота. Порядок такой: право (отказ в журнал и тревогой),
// само действие, запись в журнал. Настройки и тексты лежат в своих хранилищах с их записью, а транзакция ядра
// занимает единственное соединение записи: делать то и другое внутри неё нельзя, поэтому журнал пишется сразу после
// успешного действия. Не получилось действие: журнала нет, ошибка идёт наверх.
func (c *Core) ownerAction(ctx context.Context, actorTg int64, action string,
	do func() (target string, details map[string]string, err error)) error {
	if _, err := c.Require(ctx, actorTg, PermOwner, action); err != nil {
		return err
	}
	target, details, err := do()
	if err != nil {
		return err
	}
	return c.asAdmin(ctx, actorTg, PermOwner, action, func(Tx, Admin, time.Time) (string, map[string]string, error) {
		return target, details, nil
	})
}
