package tgsign

import (
	"crypto/sha256"
	"encoding/json"
	"net/url"
	"strconv"
	"time"
)

// User это пользователь Telegram из подписанных данных.
type User struct {
	ID           int64  `json:"id"`
	FirstName    string `json:"first_name"`
	Username     string `json:"username"`
	LanguageCode string `json:"language_code"`
}

// InitData это проверенные данные Mini App.
type InitData struct {
	User       User
	StartParam string
	AuthDate   time.Time
}

// VerifyInitData проверяет строку initData от Mini App и возвращает пользователя.
// maxAge равный нулю означает DefaultMaxAge. Время now задаёт вызывающий.
func VerifyInitData(initData, botToken string, now time.Time, maxAge time.Duration) (InitData, error) {
	var res InitData
	if botToken == "" {
		return res, ErrNoToken
	}
	v, err := url.ParseQuery(initData)
	if err != nil {
		return res, ErrBadFormat
	}
	if err := noDuplicates(v); err != nil {
		return res, err
	}
	if err := checkHash(v, hmacKey("WebAppData", botToken)); err != nil {
		return res, err
	}
	if err := checkAge(v, now, maxAge); err != nil {
		return res, err
	}
	raw := v.Get("user")
	if raw == "" {
		return res, ErrNoUser
	}
	if err := json.Unmarshal([]byte(raw), &res.User); err != nil || res.User.ID == 0 {
		return res, ErrBadFormat
	}
	res.StartParam = v.Get("start_param")
	sec, _ := strconv.ParseInt(v.Get("auth_date"), 10, 64)
	res.AuthDate = time.Unix(sec, 0)
	return res, nil
}

// hmacKey считает ключ Mini App: HMAC-SHA256(key=salt, msg=токен).
func hmacKey(salt, token string) []byte {
	return hmacSum([]byte(salt), []byte(token))
}

// widgetKey считает ключ Login Widget: SHA256(токен).
func widgetKey(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}
