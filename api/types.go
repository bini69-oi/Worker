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
		reflect.TypeFor[AdminUserList](),
		reflect.TypeFor[AdminUserCard](),
		reflect.TypeFor[AdminPaymentList](),
		reflect.TypeFor[AdminTariffList](),
		reflect.TypeFor[AdminTariff](),
		reflect.TypeFor[AdminPromoList](),
		reflect.TypeFor[AdminPromo](),
		reflect.TypeFor[AdminBroadcastList](),
		reflect.TypeFor[AdminBroadcast](),
		reflect.TypeFor[AdminSegmentCount](),
		reflect.TypeFor[AdminBalance](),
		reflect.TypeFor[AdminExpires](),
	}
}
