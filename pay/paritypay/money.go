package paritypay

import (
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
