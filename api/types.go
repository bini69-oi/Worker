package api

import "reflect"

// Types это структуры, которые фронт получает от API. По ним `make types` пишет web/src/lib/types.gen.ts.
// Новая структура ответа добавляется сюда: без этого у фронта не будет её типа.
func Types() []reflect.Type {
	return []reflect.Type{
		reflect.TypeFor[ErrorBody](),
		reflect.TypeFor[AdminLoginStarted](),
		reflect.TypeFor[AdminLoginState](),
		reflect.TypeFor[AdminMe](),
	}
}
