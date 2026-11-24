package tgsign

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// WebhookSecretHeader это заголовок, в котором Telegram присылает секрет вебхука.
const WebhookSecretHeader = "X-Telegram-Bot-Api-Secret-Token"

// DefaultMaxAge это срок жизни подписанных данных, если вызывающий не задал свой.
const DefaultMaxAge = time.Hour

// maxFutureSkew это допустимый заход auth_date вперёд из-за разницы часов.
const maxFutureSkew = time.Minute

var (
	ErrNoSecret     = errors.New("секрет вебхука не задан")
	ErrNoToken      = errors.New("токен бота не задан")
	ErrBadSecret    = errors.New("секрет вебхука не совпал")
	ErrNoHash       = errors.New("в данных нет подписи hash")
	ErrBadSignature = errors.New("подпись не сходится")
	ErrNoAuthDate   = errors.New("в данных нет корректного auth_date")
	ErrExpired      = errors.New("данные устарели, откройте приложение заново")
	ErrFromFuture   = errors.New("время данных лежит в будущем")
	ErrBadFormat    = errors.New("данные имеют неверный формат")
	ErrNoUser       = errors.New("в данных нет пользователя")
)

// CheckWebhook сверяет секретный заголовок запроса с ожидаемым секретом.
// Пустой ожидаемый секрет всегда даёт отказ.
func CheckWebhook(r *http.Request, expected string) error {
	if expected == "" {
		return ErrNoSecret
	}
	got := r.Header.Get(WebhookSecretHeader)
	if subtle.ConstantTimeCompare([]byte(got), []byte(expected)) != 1 {
		return ErrBadSecret
	}
	return nil
}

// dataCheckString собирает строку для подписи: все поля кроме hash, по ключу, через перевод строки.
func dataCheckString(v url.Values) string {
	keys := make([]string, 0, len(v))
	for k := range v {
		if k != "hash" {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	lines := make([]string, len(keys))
	for i, k := range keys {
		lines[i] = k + "=" + v.Get(k)
	}
	return strings.Join(lines, "\n")
}

// hmacSum считает HMAC-SHA256(key, msg).
func hmacSum(key, msg []byte) []byte {
	m := hmac.New(sha256.New, key)
	m.Write(msg)
	return m.Sum(nil)
}

// hmacHex считает hex(HMAC-SHA256(key, msg)).
func hmacHex(key, msg []byte) string {
	return hex.EncodeToString(hmacSum(key, msg))
}

// checkHash сверяет hash из данных с подписью по ключу за постоянное время.
func checkHash(v url.Values, key []byte) error {
	got := v.Get("hash")
	if got == "" {
		return ErrNoHash
	}
	want := hmacHex(key, []byte(dataCheckString(v)))
	if subtle.ConstantTimeCompare([]byte(strings.ToLower(got)), []byte(want)) != 1 {
		return ErrBadSignature
	}
	return nil
}

// checkAge проверяет auth_date: не старше maxAge и не из будущего больше чем на минуту.
func checkAge(v url.Values, now time.Time, maxAge time.Duration) error {
	sec, err := strconv.ParseInt(v.Get("auth_date"), 10, 64)
	if err != nil || sec <= 0 {
		return ErrNoAuthDate
	}
	if maxAge <= 0 {
		maxAge = DefaultMaxAge
	}
	at := time.Unix(sec, 0)
	if at.After(now.Add(maxFutureSkew)) {
		return ErrFromFuture
	}
	if now.Sub(at) > maxAge {
		return ErrExpired
	}
	return nil
}

// noDuplicates отклоняет данные, где один ключ повторён: подпись считалась бы по первому значению.
func noDuplicates(v url.Values) error {
	for _, vals := range v {
		if len(vals) != 1 {
			return ErrBadFormat
		}
	}
	return nil
}
