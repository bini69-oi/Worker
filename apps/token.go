package apps

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"time"
)

// TokenTTL это сколько живёт кнопка «Добавить подписку». Старая кнопка в чате через сутки не работает.
const TokenTTL = 24 * time.Hour

// ErrToken значит, что токен подделан или испорчен. ErrExpired значит, что он честный, но старый.
var (
	ErrToken   = errors.New("токен не подходит")
	ErrExpired = errors.New("токен устарел")
)

// Части токена: 8 байт id абонента, 8 байт срока, 4 байта отпечатка ссылки подписки, 16 байт подписи.
const (
	bodyLen  = 20
	tokenLen = bodyLen + 16
)

// Token делает токен перехода: id абонента, срок и отпечаток текущей ссылки подписки, подписанные HMAC.
// Ссылки подписки и id открытым текстом в адресе нет. После перевыпуска ссылки отпечаток не сходится,
// и утёкшая кнопка больше ничего не выдаёт.
func Token(key []byte, userID int64, subURL string, now time.Time) string {
	b := make([]byte, bodyLen, tokenLen)
	binary.BigEndian.PutUint64(b[:8], uint64(userID))
	binary.BigEndian.PutUint64(b[8:16], uint64(now.Add(TokenTTL).Unix()))
	copy(b[16:], Fingerprint(subURL))
	b = append(b, sign(key, b)...)
	return base64.RawURLEncoding.EncodeToString(xor(key, b[:bodyLen], b[bodyLen:]))
}

// Fingerprint это 4 байта хэша ссылки подписки: по ним видно, что ссылку перевыпустили.
func Fingerprint(subURL string) []byte {
	sum := sha256.Sum256([]byte(subURL))
	return sum[:4]
}

// Parse проверяет токен и отдаёт id абонента и отпечаток ссылки, для которой токен выдан.
func Parse(key []byte, tok string, now time.Time) (int64, []byte, error) {
	raw, err := base64.RawURLEncoding.DecodeString(tok)
	if err != nil || len(raw) != tokenLen {
		return 0, nil, ErrToken
	}
	mac := raw[bodyLen:]
	body := xor(key, raw[:bodyLen], mac)[:bodyLen]
	if !hmac.Equal(mac, sign(key, body)) {
		return 0, nil, ErrToken
	}
	if now.Unix() > int64(binary.BigEndian.Uint64(body[8:16])) {
		return 0, nil, ErrExpired
	}
	id := int64(binary.BigEndian.Uint64(body[:8]))
	if id <= 0 {
		return 0, nil, ErrToken
	}
	return id, body[16:], nil
}

func sign(key, body []byte) []byte {
	m := hmac.New(sha256.New, append([]byte("worker/dl:"), key...))
	m.Write(body)
	return m.Sum(nil)[:16]
}

// xor прячет id и срок под маской из подписи: по токену не видно, какой это абонент и сколько их.
// Маска зависит от подписи, а подпись проверяется после снятия маски, так что подделка не проходит.
func xor(key, body, mac []byte) []byte {
	m := hmac.New(sha256.New, append([]byte("worker/dl-mask:"), key...))
	m.Write(mac)
	mask := m.Sum(nil)
	out := make([]byte, 0, len(body)+len(mac))
	for i, c := range body {
		out = append(out, c^mask[i])
	}
	return append(out, mac...)
}
