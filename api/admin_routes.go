package api

import (
	"net/http"

	"github.com/bini69-oi/worker/core"
)

// adminRoute это строка таблицы прав: адрес, право на него и обработчик. Пустое право значит любого
// вошедшего админа, core.PermOwner только владельца. Все адреса админки кроме входа заведены здесь,
// поэтому проверка права не зависит от того, помнил ли о ней автор обработчика.
type adminRoute struct {
	Pattern string
	Perm    core.Perm
	H       adminHandler
}

// AdminRoute это строка таблицы прав без обработчика: её читают тесты.
type AdminRoute struct {
	Pattern string
	Perm    core.Perm
}

// routes это вся таблица. Новый адрес добавляется сюда с правом, иначе его нет.
func (a *adminAPI) routes() []adminRoute {
	return []adminRoute{
		{"GET /api/admin/me", "", a.me},
		{"POST /api/admin/logout", "", a.logout},

		{"GET /api/admin/users", core.PermUsers, a.usersList},
		{"GET /api/admin/users/{id}", core.PermUsers, a.userCard},
		{"POST /api/admin/users/{id}/balance", core.PermGrant, a.userBalance},
		{"POST /api/admin/users/{id}/days", core.PermGrant, a.userDays},
		{"POST /api/admin/users/{id}/ban", core.PermBan, a.userBan(true)},
		{"POST /api/admin/users/{id}/unban", core.PermBan, a.userBan(false)},
		{"POST /api/admin/users/{id}/message", core.PermSupport, a.userMessage},
		{"POST /api/admin/users/{id}/devices/reset", core.PermGrant, a.userDevicesReset},
		{"POST /api/admin/users/{id}/link/revoke", core.PermGrant, a.userLinkRevoke},

		{"GET /api/admin/tariffs", core.PermTariffs, a.tariffsList},
		{"POST /api/admin/tariffs", core.PermTariffs, a.tariffSave},
		{"POST /api/admin/tariffs/{id}", core.PermTariffs, a.tariffSave},

		{"GET /api/admin/promos", core.PermPromo, a.promosList},
		{"POST /api/admin/promos", core.PermPromo, a.promoSave},
		{"POST /api/admin/promos/{id}", core.PermPromo, a.promoSave},
		{"POST /api/admin/promos/{id}/active", core.PermPromo, a.promoActive},

		{"GET /api/admin/broadcasts", core.PermBroadcast, a.broadcastsList},
		{"GET /api/admin/broadcasts/segment", core.PermBroadcast, a.broadcastSegment},
		{"GET /api/admin/broadcasts/{id}", core.PermBroadcast, a.broadcastGet},
		{"POST /api/admin/broadcasts", core.PermBroadcast, a.broadcastCreate},
		{"POST /api/admin/broadcasts/{id}/test", core.PermBroadcast, a.broadcastTest},
		{"POST /api/admin/broadcasts/{id}/start", core.PermBroadcast, a.broadcastStart},
		{"POST /api/admin/broadcasts/{id}/cancel", core.PermBroadcast, a.broadcastCancel},

		{"GET /api/admin/payments", core.PermStats, a.paymentsList},
		{"POST /api/admin/payments/{id}/confirm", core.PermGrant, a.paymentConfirm},
		{"POST /api/admin/payments/{id}/refund", core.PermOwner, a.paymentRefund},
	}
}

// AdminRoutes отдаёт таблицу прав адресов админки.
func AdminRoutes() []AdminRoute {
	var out []AdminRoute
	for _, rt := range (&adminAPI{}).routes() {
		out = append(out, AdminRoute{Pattern: rt.Pattern, Perm: rt.Perm})
	}
	return out
}

// guarded оборачивает обработчик: сессия, право из таблицы (ядро), потом сам обработчик.
func (a *adminAPI) guarded(rt adminRoute) Handler {
	return a.auth(func(w http.ResponseWriter, r *http.Request, ad adminCtx) (any, error) {
		if _, err := a.d.Core.Require(r.Context(), ad.Admin.TgID, rt.Perm, rt.Pattern); err != nil {
			return nil, err
		}
		return rt.H(w, r, ad)
	})
}
