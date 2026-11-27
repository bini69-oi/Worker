package api

import "reflect"

// Types это структуры, которые фронт получает от API. По ним `make types` пишет web/src/lib/types.gen.ts.
// Новая структура ответа добавляется сюда: без этого у фронта не будет её типа.
func Types() []reflect.Type {
	return append([]reflect.Type{
		reflect.TypeFor[ErrorBody](),
	}, appTypes()...)
}
