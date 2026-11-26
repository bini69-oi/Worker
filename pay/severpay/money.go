package severpay

import (
	"fmt"
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
