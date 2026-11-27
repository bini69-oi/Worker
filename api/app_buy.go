package api

import (
	"net/http"

	"github.com/bini69-oi/worker/core"
)

func (a *App) buyRoutes() {
	a.handle("POST /api/app/trial", a.trial)
	a.handle("GET /api/app/tariffs", a.tariffs)
	a.handle("POST /api/app/quote", a.quote)
	a.handle("POST /api/app/buy", a.buy)
}

// handle заводит адрес, который требует сессию и во время техработ отвечает ErrMaintenance.
func (a *App) handle(pattern string, h AuthHandler) { a.srv.Handle(pattern, a.auth(h, true)) }

func (a *App) trial(w http.ResponseWriter, r *http.Request, au Auth) (any, error) {
	var in EmptyRequest
	if err := Decode(w, r, &in); err != nil {
		return nil, err
	}
	sub, err := a.Core.Trial(r.Context(), au.User.ID)
	if err != nil {
		return nil, err
	}
	return a.subView(r, sub)
}

func (a *App) tariffs(_ http.ResponseWriter, r *http.Request, _ Auth) (any, error) {
	list, err := a.Core.Tariffs(r.Context())
	if err != nil {
		return nil, err
	}
	out := TariffsResponse{Tariffs: []TariffView{}}
	for _, t := range list {
		v := TariffView{ID: t.ID, Name: t.Name, TrafficBytes: t.TrafficBytes, Devices: t.Devices, Periods: []PeriodView{}}
		for _, p := range t.Periods {
			v.Periods = append(v.Periods, PeriodView{Days: p.Days, Price: p.Price})
		}
		out.Tariffs = append(out.Tariffs, v)
	}
	return out, nil
}

// choice разбирает тело «тариф и срок» для расчёта. Нулевые и отрицательные значения это неверные данные.
func choice(w http.ResponseWriter, r *http.Request) (QuoteRequest, error) {
	var in QuoteRequest
	if err := Decode(w, r, &in); err != nil {
		return in, err
	}
	if in.TariffID <= 0 || in.Days <= 0 || in.Days > 3650 {
		return in, core.ErrInvalid
	}
	return in, nil
}

func (a *App) quote(w http.ResponseWriter, r *http.Request, au Auth) (any, error) {
	in, err := choice(w, r)
	if err != nil {
		return nil, err
	}
	q, err := a.Core.Quote(r.Context(), au.User.ID, in.TariffID, in.Days)
	if err != nil {
		return nil, err
	}
	out := QuoteResponse{Price: q.Price, Balance: q.Balance, Missing: q.Missing, Queued: q.Queued}
	if q.Queued {
		out.StartsAt = &q.StartsAt
	}
	return out, nil
}

// buy покупает тариф. Ключ повтора обязателен: двойной клик и повтор после «Нет связи» не спишут второй раз.
// Не хватает денег: ядро запоминает выбор (после пополнения подписка выдастся сама), ответ несёт цену, баланс и недостачу.
func (a *App) buy(w http.ResponseWriter, r *http.Request, au Auth) (any, error) {
	var in BuyRequest
	if err := Decode(w, r, &in); err != nil {
		return nil, err
	}
	if in.TariffID <= 0 || in.Days <= 0 || in.Days > 3650 {
		return nil, core.ErrInvalid
	}
	o, err := a.Core.BuyOrRemember(r.Context(), au.User.ID, in.TariffID, in.Days, in.Key)
	if err != nil {
		return nil, err
	}
	if o.Short != nil {
		return nil, &core.NeedMoney{Price: o.Short.Price, Balance: o.Short.Balance, Missing: o.Short.Missing}
	}
	v, err := a.subView(r, o.Sub)
	if err != nil {
		return nil, err
	}
	return BuyResponse{Sub: *v, Queued: o.Queued}, nil
}
