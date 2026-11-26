package stripe

import (
	"errors"
	"strings"
)

// minorFor переводит копейки в минимальные единицы валюты Stripe (центы) с округлением вверх, в пользу магазина.
// rate это копеек за одну основную единицу валюты: 9500 значит 95 рублей за доллар. Валюта с двумя знаками.
// Формула целая: ceil(копейки * 100 / rate), без float.
func minorFor(kopecks, rate int64) (int64, error) {
	if kopecks <= 0 || rate <= 0 {
		return 0, errors.New("сумма или курс Stripe не положительные")
	}
	if kopecks > 1<<40 {
		return 0, errors.New("сумма для Stripe слишком большая")
	}
	return (kopecks*100 + rate - 1) / rate, nil
}

// currencyCode приводит валюту к нижнему регистру для Stripe. Нужны ровно три латинские буквы.
func currencyCode(s string) (string, bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	if len(s) != 3 {
		return "", false
	}
	for _, r := range s {
		if r < 'a' || r > 'z' {
			return "", false
		}
	}
	if !twoDecimals(s) {
		return "", false
	}
	return s, true
}

// notTwoDecimals это валюты Stripe без центов или с тремя знаками после запятой. Пересчёт в «центы»
// для них ошибся бы в 10-100 раз, поэтому такие валюты касса не принимает.
var notTwoDecimals = map[string]bool{
	"bif": true, "clp": true, "djf": true, "gnf": true, "jpy": true, "kmf": true, "krw": true, "mga": true,
	"pyg": true, "rwf": true, "ugx": true, "vnd": true, "vuv": true, "xaf": true, "xof": true, "xpf": true,
	"bhd": true, "jod": true, "kwd": true, "omr": true, "tnd": true,
}

func twoDecimals(code string) bool { return !notTwoDecimals[code] }
