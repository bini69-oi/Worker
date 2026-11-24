package settings

import (
	"context"
	"log/slog"
	"time"
)

// RefreshEvery это как часто кэш перечитывает таблицу. Спека обещает не больше 5 секунд.
const RefreshEvery = 3 * time.Second

// Run перечитывает таблицу целиком на каждый сигнал из tick, пока не отменён ctx.
// Настоящий тикер создаёт вызывающий (time.NewTicker(RefreshEvery)), в тесте сигналы шлёт тест.
// Таблица крошечная, поэтому читать её целиком проще и надёжнее, чем следить за временем правки:
// при удалении строки время правки не меняется.
func (s *Settings) Run(ctx context.Context, tick <-chan time.Time) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick:
			if err := s.Reload(); err != nil {
				// Не падаем: до следующего сигнала остаётся прежний кэш.
				slog.Warn("настройки: перечитать не вышло", "err", err)
			}
		}
	}
}
