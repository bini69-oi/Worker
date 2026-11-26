package severpay

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// rubles превращает копейки в рубли как число JSON, без float: 30000 это "300.0", 12345 это "123.45".
// Так же печатает такую сумму Python (float), а касса сверяет подпись по канону своего разбора.
func rubles(kop int64) string {
	s := fmt.Sprintf("%d.%02d", kop/100, kop%100)
	s = strings.TrimRight(s, "0")
	if strings.HasSuffix(s, ".") {
		s += "0"
	}
	return s
}

// kopecks разбирает рубли вида "300", "300.5" или "300.00" в копейки без float.
// Доли копейки, минус и экспонента считаются ошибкой.
func kopecks(s string) (int64, error) {
	bad := errors.New("сумма в ответе SeverPay не разобрана")
	whole, frac, _ := strings.Cut(strings.TrimSpace(s), ".")
	if len(frac) > 2 {
		if strings.Trim(frac[2:], "0") != "" {
			return 0, bad
		}
		frac = frac[:2]
	}
	frac += strings.Repeat("0", 2-len(frac))
	w, err := strconv.ParseInt(whole, 10, 64)
	if err != nil || w < 0 {
		return 0, bad
	}
	f, err := strconv.ParseInt(frac, 10, 64)
	if err != nil || f < 0 {
		return 0, bad
	}
	return w*100 + f, nil
}
