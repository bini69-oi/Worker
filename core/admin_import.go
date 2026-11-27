package core

import "context"

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
