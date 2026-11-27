package api

import (
	"net/http"

	"github.com/bini69-oi/worker/core"
)

func (a *App) planRoutes() {
	a.handle("POST /api/app/plan/cancel", a.cancelPlan)
	a.handle("POST /api/app/addons/traffic", a.buyTraffic)
	a.handle("POST /api/app/addons/device", a.buyDevice)
}

// addonsView собирает то, что абонент может докупить сейчас. Пусто: кнопок докупки нет.
func (a *App) addonsView(r *http.Request, userID int64) (AddonsView, error) {
	offer, err := a.Core.Addons(r.Context(), userID)
	if err != nil {
		return AddonsView{}, err
	}
	out := AddonsView{Traffic: []PackView{}, DevicePrice: offer.DevicePrice}
	for _, p := range offer.Traffic {
		out.Traffic = append(out.Traffic, PackView{GB: p.GB, Price: p.Price})
	}
	return out, nil
}

// cancelPlan отменяет оплаченную смену тарифа, деньги возвращаются на баланс. Ключ повтора обязателен: двойной клик
// возвращает деньги один раз.
func (a *App) cancelPlan(w http.ResponseWriter, r *http.Request, au Auth) (any, error) {
	var in KeyRequest
	if err := Decode(w, r, &in); err != nil {
		return nil, err
	}
	if _, err := a.Core.CancelPlanOnce(r.Context(), au.User.ID, in.Key); err != nil {
		return nil, err
	}
	return a.currentSub(r, au)
}

func (a *App) buyTraffic(w http.ResponseWriter, r *http.Request, au Auth) (any, error) {
	var in TrafficRequest
	if err := Decode(w, r, &in); err != nil {
		return nil, err
	}
	if in.GB <= 0 || in.GB > 10000 {
		return nil, core.ErrInvalid
	}
	sub, _, err := a.Core.BuyTrafficOnce(r.Context(), au.User.ID, in.GB, in.Key)
	if err != nil {
		return nil, err
	}
	return a.subView(r, sub)
}

func (a *App) buyDevice(w http.ResponseWriter, r *http.Request, au Auth) (any, error) {
	var in KeyRequest
	if err := Decode(w, r, &in); err != nil {
		return nil, err
	}
	sub, _, err := a.Core.BuyDeviceOnce(r.Context(), au.User.ID, in.Key)
	if err != nil {
		return nil, err
	}
	return a.subView(r, sub)
}

// currentSub отдаёт подписку абонента в том виде, в каком её показывает главная.
func (a *App) currentSub(r *http.Request, au Auth) (any, error) {
	sub, err := a.Core.Sub(r.Context(), au.User.ID)
	if err != nil {
		return nil, err
	}
	return a.subView(r, sub)
}
