package mulenpay

import (
	"bytes"
	"crypto/sha1"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"strconv"
	"strings"
)

// hookFields разбирает тело вебхука в порядке следования полей: подпись считается именно по нему.
// Возвращает значения без поля sign (два написания: как в тексте и как их печатает Python) и саму подпись.
func hookFields(body []byte) (raw, py, sign string, err error) {
	dec := json.NewDecoder(bytes.NewReader(body))
	if _, err = dec.Token(); err != nil {
		return "", "", "", err
	}
	var a, b strings.Builder
	for dec.More() {
		key, err := dec.Token()
		if err != nil {
			return "", "", "", err
		}
		var v json.RawMessage
		if err := dec.Decode(&v); err != nil {
			return "", "", "", err
		}
		if key == "sign" {
			_ = json.Unmarshal(v, &sign)
			continue
		}
		r, p := texts(v)
		a.WriteString(r)
		b.WriteString(p)
	}
	return a.String(), b.String(), sign, nil
}

// texts отдаёт значение поля как строку: «как есть» и «как str() в Python».
func texts(v json.RawMessage) (raw, py string) {
	var s string
	if json.Unmarshal(v, &s) == nil {
		return s, s
	}
	t := string(v)
	switch t {
	case "true":
		return t, "True"
	case "false":
		return t, "False"
	case "null":
		return t, "None"
	}
	if strings.ContainsAny(t, ".eE") {
		if f, err := strconv.ParseFloat(t, 64); err == nil {
			py = strconv.FormatFloat(f, 'f', -1, 64)
			if !strings.Contains(py, ".") {
				py += ".0"
			}
			return t, py
		}
	}
	return t, t
}

// signOK сверяет подпись вебхука: sha1(значения полей подряд + секрет), сравнение за постоянное время.
func signOK(raw, py, sign, secret string) bool {
	if sign == "" || secret == "" {
		return false
	}
	want := []byte(strings.ToLower(sign))
	ok := 0
	for _, joined := range []string{raw, py} {
		ok |= subtle.ConstantTimeCompare([]byte(hookSign(joined, secret)), want)
	}
	return ok == 1
}

// hookSign это sha1(значения полей подряд + секрет) в hex.
func hookSign(joined, secret string) string {
	sum := sha1.Sum([]byte(joined + secret))
	return hex.EncodeToString(sum[:])
}

// createSign это подпись создания счёта: sha1(валюта + сумма + shopId + секрет).
func createSign(currency, amount, shop, secret string) string {
	sum := sha1.Sum([]byte(currency + amount + shop + secret))
	return hex.EncodeToString(sum[:])
}
