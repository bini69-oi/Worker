package heleket

import (
	"bytes"
	"crypto/md5" // подпись Heleket это md5, схему задаёт касса
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"regexp"
	"strings"
)

// sign считает подпись Heleket: hex(md5(base64(тело) + ключ)).
func sign(body []byte, key string) string {
	sum := md5.Sum(append([]byte(base64.StdEncoding.EncodeToString(body)), key...))
	return hex.EncodeToString(sum[:])
}

// jsonBody собирает тело запроса: без экранирования < > & и с "\/" вместо "/", как пишет PHP.
// Подпись считается по этим же байтам, которые уходят в запрос.
func jsonBody(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	raw := bytes.TrimRight(buf.Bytes(), "\n")
	return bytes.ReplaceAll(raw, []byte("/"), []byte(`\/`)), nil
}

var (
	signLast  = regexp.MustCompile(`,\s*"sign"\s*:\s*"([0-9a-fA-F]+)"\s*}\s*$`)
	signFirst = regexp.MustCompile(`^\s*{\s*"sign"\s*:\s*"([0-9a-fA-F]+)"\s*,`)
)

// splitSign отделяет подпись от тела вебхука. Тело остаётся теми же байтами, что прислала касса,
// без поля sign: пересобирать JSON нельзя, порядок и экранирование должны сохраниться.
func splitSign(raw []byte) (body []byte, got string, ok bool) {
	if m := signLast.FindSubmatchIndex(raw); m != nil {
		return append(append([]byte{}, raw[:m[0]]...), '}'), string(raw[m[2]:m[3]]), true
	}
	if m := signFirst.FindSubmatchIndex(raw); m != nil {
		return append([]byte{'{'}, raw[m[1]:]...), string(raw[m[2]:m[3]]), true
	}
	return nil, "", false
}

// validSignature сверяет подпись вебхука за постоянное время. Касса подписывает JSON в записи PHP, а в какой
// именно (слэши экранированы или нет) по документации неясно, поэтому годится любая из двух.
func validSignature(raw []byte, key string) bool {
	body, got, ok := splitSign(raw)
	if !ok || key == "" {
		return false
	}
	var compact bytes.Buffer
	if json.Compact(&compact, body) != nil {
		return false
	}
	plain := strings.ReplaceAll(compact.String(), `\/`, "/")
	escaped := strings.ReplaceAll(plain, "/", `\/`)
	got = strings.ToLower(got)
	ok1 := subtle.ConstantTimeCompare([]byte(got), []byte(sign([]byte(escaped), key)))
	ok2 := subtle.ConstantTimeCompare([]byte(got), []byte(sign([]byte(plain), key)))
	return ok1|ok2 == 1
}
