package core

import (
	"strconv"
	"strings"
)

// MaxTopup это предел суммы пополнения в копейках: защита от опечатки «1000000000». Один для бота и кабинета.
const MaxTopup int64 = 1_000_000 * 100

// TopupPresets отдаёт готовые суммы пополнения в копейках из настройки pay.presets. Неверные и слишком большие пропускаются.
func (c *Core) TopupPresets() []int64 {
	var out []int64
	for _, p := range strings.Split(c.cfg.String("pay.presets"), ",") {
		rub, err := strconv.ParseInt(strings.TrimSpace(p), 10, 64)
		if err != nil || rub <= 0 || rub*100 > MaxTopup {
			continue
		}
		out = append(out, rub*100)
	}
	return out
}
