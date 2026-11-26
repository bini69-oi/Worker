package aurapay

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// rubles превращает копейки в рубли с точкой, без float: 12345 это 123.45.
func rubles(kop int64) string {
	return fmt.Sprintf("%d.%02d", kop/100, kop%100)
}

// kopecks разбирает рубли вида "300", "300.5" или "1250.00" в копейки без float.
// Доли копейки, минус и экспонента считаются ошибкой.
func kopecks(s string) (int64, error) {
	whole, frac, _ := strings.Cut(strings.TrimSpace(s), ".")
	if len(frac) > 2 {
		if strings.Trim(frac[2:], "0") != "" {
			return 0, errors.New("сумма AuraPay с долями копейки")
		}
		frac = frac[:2]
	}
	frac += strings.Repeat("0", 2-len(frac))
	w, errW := strconv.ParseUint(whole, 10, 62)
	f, errF := strconv.ParseUint(frac, 10, 8)
	if errW != nil || errF != nil {
		return 0, errors.New("сумма AuraPay не разобрана")
	}
	return int64(w)*100 + int64(f), nil
}
