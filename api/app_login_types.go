package api

import "time"

// LoginStartResponse это начатый вход в браузере: id для опроса, код для сверки и ссылка на бота.
type LoginStartResponse struct {
	ID        string    `json:"id"`
	Code      string    `json:"code"`
	Link      string    `json:"link"`
	ExpiresAt time.Time `json:"expires_at"`
}

// LoginPollResponse это состояние входа: pending, confirmed, rejected или expired.
type LoginPollResponse struct {
	State string `json:"state"`
}
