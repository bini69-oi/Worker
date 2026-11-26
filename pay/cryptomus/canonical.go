package cryptomus

import "bytes"

// canonical собирает строку, которую Cryptomus подписывает в вебхуке: тело без поля sign,
// без пробелов, а слэши в строках как у PHP json_encode: "/" пишется "\/".
// Порядок полей и текст чисел и строк берём из самого тела, ничего не пересобирая через map:
// иначе порядок или запись числа разошлись бы с тем, что подписал отправитель.
// Тело уже проверено json.Unmarshal, поэтому разбираем его по байтам без проверок.
func canonical(body []byte) []byte {
	return dropSign(compact(body))
}

// compact убирает пробелы вне строк и экранирует слэши внутри строк.
func compact(body []byte) []byte {
	out := make([]byte, 0, len(body)+16)
	inStr, esc := false, false
	for _, c := range body {
		switch {
		case !inStr:
			if c == ' ' || c == '\t' || c == '\n' || c == '\r' {
				continue
			}
			inStr = c == '"'
		case esc:
			esc = false
		case c == '\\':
			esc = true
		case c == '"':
			inStr = false
		case c == '/':
			out = append(out, '\\')
		}
		out = append(out, c)
	}
	return out
}

// dropSign вырезает из объекта верхнего уровня члены с ключом "sign" вместе с одной запятой.
func dropSign(c []byte) []byte {
	out := make([]byte, 0, len(c))
	depth := 0
	for i := 0; i < len(c); i++ {
		switch c[i] {
		case '{', '[':
			depth++
		case '}', ']':
			depth--
		case '"':
			end := stringEnd(c, i)
			if depth == 1 && (last(out) == '{' || last(out) == ',') && bytes.HasPrefix(c[i:], []byte(`"sign":`)) {
				k := valueEnd(c, i+len(`"sign":`))
				if k < len(c) && c[k] == ',' {
					k++ // запятая после члена уходит вместе с ним
				} else if last(out) == ',' {
					out = out[:len(out)-1] // последний член: уходит запятая перед ним
				}
				i = k - 1
				continue
			}
			out = append(out, c[i:end+1]...)
			i = end
			continue
		}
		out = append(out, c[i])
	}
	return out
}

func last(b []byte) byte {
	if len(b) == 0 {
		return 0
	}
	return b[len(b)-1]
}

// stringEnd это индекс закрывающей кавычки строки, которая начинается в i.
func stringEnd(c []byte, i int) int {
	for j := i + 1; j < len(c); j++ {
		switch c[j] {
		case '\\':
			j++
		case '"':
			return j
		}
	}
	return len(c) - 1
}

// valueEnd это индекс запятой или скобки, которой кончается значение, начатое в i.
func valueEnd(c []byte, i int) int {
	depth := 0
	for j := i; j < len(c); j++ {
		switch c[j] {
		case '"':
			j = stringEnd(c, j)
		case '{', '[':
			depth++
		case '}', ']':
			if depth == 0 {
				return j
			}
			depth--
		case ',':
			if depth == 0 {
				return j
			}
		}
	}
	return len(c)
}
