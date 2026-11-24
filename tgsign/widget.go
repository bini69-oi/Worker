package tgsign

import (
	"net/url"
	"strconv"
	"time"
)

// VerifyLogin проверяет поля, которые прислал Telegram Login Widget, и возвращает пользователя.
// maxAge равный нулю означает DefaultMaxAge. Время now задаёт вызывающий.
func VerifyLogin(fields url.Values, botToken string, now time.Time, maxAge time.Duration) (User, error) {
	var u User
	if botToken == "" {
		return u, ErrNoToken
	}
	if err := noDuplicates(fields); err != nil {
		return u, err
	}
	if err := checkHash(fields, widgetKey(botToken)); err != nil {
		return u, err
	}
	if err := checkAge(fields, now, maxAge); err != nil {
		return u, err
	}
	id, err := strconv.ParseInt(fields.Get("id"), 10, 64)
	if err != nil || id == 0 {
		return u, ErrNoUser
	}
	u.ID = id
	u.FirstName = fields.Get("first_name")
	u.Username = fields.Get("username")
	return u, nil
}
