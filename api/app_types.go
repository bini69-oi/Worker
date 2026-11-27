package api

import (
	"reflect"
	"time"
)

// Запросы и ответы кабинета. Структуры ответов попадают в types.gen.ts через appTypes.

// EmptyRequest это тело запроса без полей: `{}`.
type EmptyRequest struct{}

// SessionTgRequest это вход из Mini App.
type SessionTgRequest struct {
	InitData string `json:"init_data"`
}

// SessionResponse это сессия Mini App: токен идёт заголовком Authorization.
type SessionResponse struct {
	Token     string    `json:"token"`
	Lang      string    `json:"lang"`
	ExpiresAt time.Time `json:"expires_at"`
}

func appTypes() []reflect.Type {
	return []reflect.Type{
		reflect.TypeFor[SessionResponse](),
		reflect.TypeFor[LoginStartResponse](),
		reflect.TypeFor[LoginPollResponse](),
	}
}
