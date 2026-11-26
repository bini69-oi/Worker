package paritypay

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// money это сумма в рублях: в JSON может быть строкой "1250.00" или числом 1500.5. Храним текст, без float.
type money string

func (m *money) UnmarshalJSON(b []byte) error {
	s := strings.TrimSpace(string(b))
	if s == "null" {
		return nil
	}
	*m = money(strings.Trim(s, `"`))
	return nil
}

// rubles превращает копейки в строку рублей без float: 12345 это "123.45".
func rubles(kop int64) string {
	return fmt.Sprintf("%d.%02d", kop/100, kop%100)
}

// kopecks разбирает рубли вида "300", "300.5" или "300.50" в копейки без float.
// Доли копейки, минус и пустое значение это ошибка: округлять деньги нельзя.
func kopecks(s string) (int64, error) {
	errBad := errors.New("сумма в ответе ParityPay не разобрана")
	whole, frac, _ := strings.Cut(strings.TrimSpace(s), ".")
	if len(frac) > 2 {
		if strings.Trim(frac[2:], "0") != "" {
			return 0, errors.New("сумма в ответе ParityPay с долями копейки")
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
	if w == 0 && f == 0 {
		return 0, errBad
	}
	return w*100 + f, nil
}
