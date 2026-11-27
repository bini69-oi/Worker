package api

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/bini69-oi/worker/core"
)

func (a *App) meRoutes() {
	a.handleOpen("GET /api/app/me", a.me)
	a.srv.Handle("GET /api/app/texts", a.texts)
}

const gib = 1 << 30

// me это главная: абонент, подписка, баланс, доступен ли пробный, кассы. Работает и во время техработ:
// страница по признаку Maintenance показывает текст о работах.
func (a *App) me(w http.ResponseWriter, r *http.Request, au Auth) (any, error) {
	ctx := r.Context()
	out := MeResponse{Lang: au.User.Lang, Username: au.User.Username, Balance: au.User.Balance,
		Gateways: []GatewayView{}, BotLink: a.botLink()}
	out.Maintenance = errors.Is(a.Core.Serving(ctx, au.User.ID), core.ErrMaintenance)
	for _, g := range a.Core.Gateways() {
		out.Gateways = append(out.Gateways, GatewayView{Name: g.Name, Title: g.Title})
	}
	sub, err := a.Core.Sub(ctx, au.User.ID)
	if err != nil && !errors.Is(err, core.ErrNotFound) {
		return nil, err
	}
	hasSub := err == nil
	out.TrialAvailable = a.Core.TrialOpen(au.User, hasSub)
	if hasSub {
		if out.Sub, err = a.subView(r, sub); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (a *App) botLink() string {
	if name := a.BotName(); name != "" {
		return "https://t.me/" + name
	}
	return ""
}

// subView собирает подписку для показа: состояние, дни, лимиты и ждущая смена тарифа.
func (a *App) subView(r *http.Request, s core.Subscription) (*SubView, error) {
	now := a.now()
	v := &SubView{Trial: s.Trial, Imported: s.Imported, ExpiresAt: s.ExpiresAt, AutoRenew: s.AutoRenew,
		ExtraTrafficBytes: s.ExtraTrafficBytes, ExtraDevices: s.ExtraDevices}
	switch {
	case s.Status != core.SubActive || !s.ExpiresAt.After(now):
		v.State = "expired"
	case s.Trial:
		v.State = "trial"
	default:
		v.State = "active"
	}
	if v.State != "expired" {
		v.DaysLeft = int((s.ExpiresAt.Sub(now) + 24*time.Hour - 1) / (24 * time.Hour))
	}
	switch {
	case s.Imported:
		v.TrafficBytes, v.Devices = core.Limits(s, core.Tariff{})
	case s.Trial:
		v.TrafficBytes, v.Devices = a.Config.Int("trial.traffic_gb")*gib, int(a.Config.Int("trial.devices"))
	default:
		t, err := a.Core.Tariff(r.Context(), s.TariffID)
		if err != nil {
			return nil, err
		}
		v.TariffID, v.Tariff = t.ID, t.Name
		v.TrafficBytes, v.Devices = core.Limits(s, t)
	}
	if s.NextTariffID != 0 {
		if t, err := a.Core.Tariff(r.Context(), s.NextTariffID); err == nil {
			v.Next = &NextPlanView{TariffID: t.ID, Tariff: t.Name, Days: s.NextDays, StartsAt: s.ExpiresAt}
		}
	}
	return v, nil
}

// texts отдаёт тексты кабинета и ошибок на языке абонента. Работает и без сессии: страница входа тоже
// показывает тексты. Тогда язык берётся из параметра lang, потом из Accept-Language.
func (a *App) texts(w http.ResponseWriter, r *http.Request) (any, error) {
	lang := ""
	if au, err := a.authenticate(w, r); err == nil {
		lang = au.User.Lang
	}
	if lang == "" {
		lang = a.guessLang(r)
	}
	return TextsResponse{Lang: lang, Texts: a.Texts(lang, "app.", "err.")}, nil
}

// guessLang выбирает язык для страницы без сессии: параметр lang, если это ru или en, иначе по Accept-Language.
func (a *App) guessLang(r *http.Request) string {
	if q := r.URL.Query().Get("lang"); q == "ru" || q == "en" {
		return q
	}
	h, _, _ := strings.Cut(r.Header.Get("Accept-Language"), ",")
	return a.Core.Lang(h)
}
