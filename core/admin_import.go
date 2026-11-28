package core

import (
	"context"
	"time"
)

// importPreviewEvery это как часто можно просить пробный прогон: он читает всю панель.
const importPreviewEvery = time.Minute

// RequestImport просит владельца подтвердить импорт абонентов из панели кнопкой «Да» в Telegram. Только владелец.
// Сначала страница показывает отчёт пробного прогона (ImportPanel без записи), запись идёт после «Да»: та же
// функция, что у команды `worker import panel`. Итог импорта пишется в журнал действий.
func (c *Core) RequestImport(ctx context.Context, actorTg int64) (int64, error) {
	if _, err := c.Require(ctx, actorTg, PermOwner, "import.request"); err != nil {
		return 0, err
	}
	return c.RequestAction(ctx, actorTg, "import", struct{}{})
}

// onImportApproved выполняет импорт после «Да» владельца.
func (c *Core) onImportApproved(ctx context.Context, _ []byte) error {
	_, err := c.ImportPanel(ctx, c.owner, true)
	return err
}

// PreviewImport это пробный прогон импорта для админки: отчёт без записи. Только владелец. Прогон читает всех
// пользователей панели, поэтому идёт не чаще раза в минуту и не больше одного за раз: иначе ErrTooManyTries.
func (c *Core) PreviewImport(ctx context.Context, actorTg int64) (ImportReport, error) {
	if _, err := c.Require(ctx, actorTg, PermOwner, "import.preview"); err != nil {
		return ImportReport{}, err
	}
	if !c.importMu.TryLock() {
		return ImportReport{}, ErrTooManyTries
	}
	defer c.importMu.Unlock()
	now := c.clock.Now()
	if last, ok := c.importAt.Load().(time.Time); ok && now.Sub(last) < importPreviewEvery {
		return ImportReport{}, ErrTooManyTries
	}
	c.importAt.Store(now)
	return c.ImportPanel(ctx, actorTg, false)
}
