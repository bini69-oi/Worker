package severpay

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"
)

// canon собирает строку, которую подписывает касса: ключи верхнего уровня по алфавиту, JSON без пробелов,
// значения как есть (числа и кириллица не пересобираются). Поле sign в подпись не входит.
// deep сортирует ещё и вложенные объекты: касса может подписывать так, и мы пробуем оба канона.
func canon(m map[string]json.RawMessage, deep bool) []byte {
	keys := make([]string, 0, len(m))
	for k := range m {
		if k != "sign" {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	var b bytes.Buffer
	b.WriteByte('{')
	for i, k := range keys {
		if i > 0 {
			b.WriteByte(',')
		}
		b.Write(quote(k))
		b.WriteByte(':')
		b.Write(value(m[k], deep))
	}
	b.WriteByte('}')
	return b.Bytes()
}

// value отдаёт значение без пробелов; вложенный объект при deep сортируется по ключам.
func value(raw json.RawMessage, deep bool) []byte {
	var out bytes.Buffer
	if err := json.Compact(&out, raw); err != nil {
		return raw
	}
	if !deep {
		return out.Bytes()
	}
	var inner map[string]json.RawMessage
	if json.Unmarshal(raw, &inner) != nil {
		return out.Bytes()
	}
	for k, v := range inner {
		inner[k] = value(v, true)
	}
	return canon(inner, true)
}

// quote кодирует строку JSON без экранирования < > &: Python их не трогает.
func quote(v string) []byte {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
	return bytes.TrimRight(b.Bytes(), "\n")
}

// mac это HMAC-SHA256 канона ключом кассы в hex.
func mac(canonical []byte, token string) string {
	h := hmac.New(sha256.New, []byte(token))
	h.Write(canonical)
	return hex.EncodeToString(h.Sum(nil))
}

// signed считает подпись и отдаёт тело запроса: канон с дописанным sign. Подписаны и отправлены одни байты.
func signed(m map[string]json.RawMessage, token string) []byte {
	body := canon(m, false)
	sig := quote(mac(body, token))
	return append(body[:len(body)-1], []byte(`,"sign":`+string(sig)+"}")...)
}

// equalSign сравнивает подписи за постоянное время без учёта регистра hex.
func equalSign(got, want string) bool {
	return subtle.ConstantTimeCompare([]byte(strings.ToLower(got)), []byte(strings.ToLower(want))) == 1
}
