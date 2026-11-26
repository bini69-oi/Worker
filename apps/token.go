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

// tokenLen это 8 байт id абонента, 8 байт срока и 16 байт подписи.
const tokenLen = 32

// Token делает токен перехода: id абонента и срок, подписанные HMAC. Ссылки подписки и id
// открытым текстом в адресе нет.
func Token(key []byte, userID int64, now time.Time) string {
	b := make([]byte, 16, tokenLen)
	binary.BigEndian.PutUint64(b[:8], uint64(userID))
	binary.BigEndian.PutUint64(b[8:], uint64(now.Add(TokenTTL).Unix()))
	b = append(b, sign(key, b)...)
	return base64.RawURLEncoding.EncodeToString(xor(key, b[:16], b[16:]))
}

// Parse проверяет токен и отдаёт id абонента.
func Parse(key []byte, tok string, now time.Time) (int64, error) {
	raw, err := base64.RawURLEncoding.DecodeString(tok)
	if err != nil || len(raw) != tokenLen {
		return 0, ErrToken
	}
	mac := raw[16:]
	body := xor(key, raw[:16], mac)[:16]
	if !hmac.Equal(mac, sign(key, body)) {
		return 0, ErrToken
	}
	if now.Unix() > int64(binary.BigEndian.Uint64(body[8:])) {
		return 0, ErrExpired
	}
	id := int64(binary.BigEndian.Uint64(body[:8]))
	if id <= 0 {
		return 0, ErrToken
	}
	return id, nil
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
