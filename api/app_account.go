package api

import (
	"net/http"
	"strings"

	"github.com/bini69-oi/worker/core"
)

func (a *App) accountRoutes() {
	a.handle("GET /api/app/history", a.history)
	a.handle("GET /api/app/ref", a.ref)
	a.handle("POST /api/app/promo", a.promo)
	a.handle("POST /api/app/autorenew", a.autoRenew)
}

// historyLimit это сколько последних движений денег отдаёт API.
const historyLimit = 50

// history отдаёт движения баланса. Причина кодом, текст по ней показывает страница. Комментарии админов
// наружу не идут: в них бывают внутренние пометки.
func (a *App) history(_ http.ResponseWriter, r *http.Request, au Auth) (any, error) {
	list, err := a.Core.History(r.Context(), au.User.ID, historyLimit)
	if err != nil {
		return nil, err
	}
	out := HistoryResponse{Items: []HistoryItem{}}
	for _, e := range list {
		out.Items = append(out.Items, HistoryItem{Amount: e.Amount, Reason: string(e.Reason), CreatedAt: e.CreatedAt})
	}
	return out, nil
}

func (a *App) ref(_ http.ResponseWriter, r *http.Request, au Auth) (any, error) {
	st, err := a.Core.RefStats(r.Context(), au.User.ID)
	if err != nil {
		return nil, err
	}
	link := ""
	if b := a.botLink(); b != "" {
		link = b + "?start=ref_" + au.User.RefCode
	}
	return RefResponse{Link: link, Invited: st.Invited, Bought: st.Bought, Earned: st.Earned}, nil
}

func (a *App) promo(w http.ResponseWriter, r *http.Request, au Auth) (any, error) {
	var in PromoRequest
	if err := Decode(w, r, &in); err != nil {
		return nil, err
	}
	code := strings.TrimSpace(in.Code)
	if code == "" || len(code) > 64 {
		return nil, core.ErrPromoNotFound
	}
	p, err := a.Core.ApplyPromo(r.Context(), au.User.ID, code)
	if err != nil {
		return nil, err
	}
	return PromoResponse{Kind: string(p.Kind), Value: p.Value}, nil
}

func (a *App) autoRenew(w http.ResponseWriter, r *http.Request, au Auth) (any, error) {
	var in AutoRenewRequest
	if err := Decode(w, r, &in); err != nil {
		return nil, err
	}
	if in.On == nil {
		return nil, core.ErrInvalid
	}
	if err := a.Core.SetAutoRenew(r.Context(), au.User.ID, *in.On); err != nil {
		return nil, err
	}
	sub, err := a.Core.Sub(r.Context(), au.User.ID)
	if err != nil {
		return nil, err
	}
	return a.subView(r, sub)
}
