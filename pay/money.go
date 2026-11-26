package pay

import (
	"errors"
	"strconv"
	"strings"
)

// ErrBadAmount значит, что сумма от кассы не разобрана: не число, минус, доли копейки или слишком большая.
var ErrBadAmount = errors.New("сумма от кассы не разобрана")

// maxRubDigits это предел цифр в рублях: 12 цифр это триллион рублей, в int64 копеек помещается с запасом.
const maxRubDigits = 12

// Kopecks переводит сумму в рублях строкой в копейки без float: «300» → 30000, «300.5» → 30050,
// «300.000000» → 30000. Это чужой ввод, поэтому только цифры и одна точка, без знака и пробелов внутри.
// Ненулевые знаки после второго после точки это доли копейки: ошибка, а не округление.
func Kopecks(s string) (int64, error) {
	whole, frac, dot := strings.Cut(strings.TrimSpace(s), ".")
	if !digits(whole, 1, maxRubDigits) || !digits(frac, 0, 32) || (dot && frac == "") {
		return 0, ErrBadAmount
	}
	if len(frac) > 2 {
		if strings.Trim(frac[2:], "0") != "" {
			return 0, ErrBadAmount
		}
		frac = frac[:2]
	}
	frac += strings.Repeat("0", 2-len(frac))
	w, err := strconv.ParseInt(whole, 10, 64)
	if err != nil {
		return 0, ErrBadAmount
	}
	f, _ := strconv.ParseInt(frac, 10, 64)
	return w*100 + f, nil
}

// Rubles пишет копейки рублями с двумя знаками: 30050 → «300.50».
func Rubles(k int64) string {
	sign := ""
	if k < 0 {
		sign, k = "-", -k
	}
	return sign + strconv.FormatInt(k/100, 10) + "." + strconv.FormatInt(k%100/10, 10) + strconv.FormatInt(k%10, 10)
}

// digits говорит, что s из одних цифр и длиной от min до max.
func digits(s string, min, max int) bool {
	if len(s) < min || len(s) > max {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}
