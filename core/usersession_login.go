package core

import "time"

// UserLogin это запрос входа в браузере через бота. В базе только хэши id и nonce.
type UserLogin struct {
	ID         int64
	IDHash     []byte // хэш id, который знает только браузер
	NonceHash  []byte // хэш nonce, который знает только ссылка на бота
	Code       string // двузначный код, который показывает сайт
	TgID       int64  // кто открыл ссылку; 0, пока никто
	IP         string
	Agent      string
	CreatedAt  time.Time
	ExpiresAt  time.Time
	Answer     string // yes, no или пусто
	AnsweredAt time.Time
	UsedAt     time.Time // когда браузер забрал сессию
}
