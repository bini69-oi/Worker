package core

import "context"

// Move открывает move для внешних тестов ядра (пакет core_test).
var Move = move

// SyncSub открывает syncSub: тест зовёт синхронизацию повторно, как если бы ответ панели потерялся.
func (c *Core) SyncSub(ctx context.Context, subID int64) error { return c.syncSub(ctx, subID) }

// Backoff открывает backoff.
var Backoff = backoff
