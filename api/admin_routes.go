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
