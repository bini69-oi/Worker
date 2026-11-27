package api

import (
	"errors"
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

// cancelPlan отменяет оплаченную смену тарифа, деньги возвращаются на баланс. Двойной клик возвращает их один раз.
func (a *App) cancelPlan(w http.ResponseWriter, r *http.Request, au Auth) (any, error) {
	var in EmptyRequest
	if err := Decode(w, r, &in); err != nil {
		return nil, err
	}
	if err := a.Core.CancelPlan(r.Context(), au.User.ID); err != nil {
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
	sub, err := a.Core.BuyTraffic(r.Context(), au.User.ID, in.GB)
	if err != nil {
		return nil, a.addonMoney(r, au, err, func(o AddonsView) int64 {
			for _, p := range o.Traffic {
				if p.GB == in.GB {
					return p.Price
				}
			}
			return 0
		})
	}
	return a.subView(r, sub)
}

func (a *App) buyDevice(w http.ResponseWriter, r *http.Request, au Auth) (any, error) {
	var in EmptyRequest
	if err := Decode(w, r, &in); err != nil {
		return nil, err
	}
	sub, err := a.Core.BuyDevice(r.Context(), au.User.ID)
	if err != nil {
		return nil, a.addonMoney(r, au, err, func(o AddonsView) int64 { return o.DevicePrice })
	}
	return a.subView(r, sub)
}

// addonMoney дополняет «не хватает денег» ценой докупки, чтобы страница показала недостачу и кнопку пополнения.
func (a *App) addonMoney(r *http.Request, au Auth, err error, price func(AddonsView) int64) error {
	if !errors.Is(err, core.ErrNotEnoughMoney) {
		return err
	}
	var need *core.NeedMoney
	if !errors.As(err, &need) {
		return err
	}
	offer, oerr := a.addonsView(r, au.User.ID)
	if oerr != nil {
		return err
	}
	p := price(offer)
	return &moneyError{MoneyDetails{Price: p, Balance: max(p-need.Missing, 0), Missing: need.Missing}}
}

// currentSub отдаёт подписку абонента в том виде, в каком её показывает главная.
func (a *App) currentSub(r *http.Request, au Auth) (any, error) {
	sub, err := a.Core.Sub(r.Context(), au.User.ID)
	if err != nil {
		return nil, err
	}
	return a.subView(r, sub)
}
