package heleket

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

// kopecks разбирает рубли вида "300", "300.5" или "300.00" в копейки без float.
// Доли копейки допустимы только нулями, отрицательные и мусорные строки отклоняются.
func kopecks(s string) (int64, error) {
	bad := fmt.Errorf("сумма %q в ответе Heleket не разобрана", s)
	whole, frac, _ := strings.Cut(strings.TrimSpace(s), ".")
	if whole == "" || len(whole) > 12 || !digits(whole) || !digits(frac) {
		return 0, bad
	}
	if len(frac) > 2 {
		if strings.Trim(frac[2:], "0") != "" {
			return 0, errors.New("сумма в ответе Heleket с долями копейки")
		}
		frac = frac[:2]
	}
	frac += strings.Repeat("0", 2-len(frac))
	w, _ := strconv.ParseInt(whole, 10, 64)
	f, _ := strconv.ParseInt(frac, 10, 64)
	return w*100 + f, nil
}

func digits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
