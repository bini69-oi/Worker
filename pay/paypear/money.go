package paypear

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// rubles превращает копейки в строку рублей без float: 12345 это "123.45".
func rubles(kop int64) string {
	return fmt.Sprintf("%d.%02d", kop/100, kop%100)
}

// kopecks разбирает рубли вида "300", "300.5" или "300.50" в копейки без float.
func kopecks(s string) (int64, error) {
	errBad := errors.New("сумма в ответе PayPear не разобрана")
	whole, frac, _ := strings.Cut(strings.TrimSpace(s), ".")
	if len(frac) > 2 {
		if strings.Trim(frac[2:], "0") != "" {
			return 0, errors.New("сумма в ответе PayPear с долями копейки")
		}
		frac = frac[:2]
	}
	frac += strings.Repeat("0", 2-len(frac))
	w, err := strconv.ParseInt(whole, 10, 64)
	if err != nil || w < 0 {
		return 0, errBad
	}
	f, err := strconv.ParseInt(frac, 10, 64)
	if err != nil || f < 0 {
		return 0, errBad
	}
	return w*100 + f, nil
}
