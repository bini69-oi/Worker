package api

import (
	"errors"
	"net/http"
	"net/url"
	"time"

	"github.com/bini69-oi/worker/core"
)

// AdminPeriod это срок тарифа и цена в копейках.
type AdminPeriod struct {
	Days  int   `json:"days"`
	Price int64 `json:"price"`
}

// AdminTariff это тариф для правки: все поля, которые меняет админ.
type AdminTariff struct {
	ID           int64         `json:"id"`
	Name         string        `json:"name"`
	TrafficBytes int64         `json:"traffic_bytes"` // 0 без лимита
	Devices      int           `json:"devices"`       // 0 без лимита
	Reset        string        `json:"reset"`
	Squads       []string      `json:"squads"`
	Active       bool          `json:"active"`
	Sort         int           `json:"sort"`
	Periods      []AdminPeriod `json:"periods"`
}

// AdminSquad это сквад панели для выбора галочкой.
type AdminSquad struct {
	UUID    string `json:"uuid"`
	Name    string `json:"name"`
	Members int    `json:"members"`
}

// AdminTariffList это все тарифы (и скрытые) и сквады панели. SquadsOK ложно, если панель не ответила.
type AdminTariffList struct {
	Tariffs  []AdminTariff `json:"tariffs"`
	Squads   []AdminSquad  `json:"squads"`
	SquadsOK bool          `json:"squads_ok"`
}

func tariffDTO(t core.Tariff) AdminTariff {
	out := AdminTariff{ID: t.ID, Name: t.Name, TrafficBytes: t.TrafficBytes, Devices: t.Devices, Reset: t.Reset,
		Squads: append([]string{}, t.Squads...), Active: t.Active, Sort: t.Sort, Periods: make([]AdminPeriod, 0, len(t.Periods))}
	for _, p := range t.Periods {
		out.Periods = append(out.Periods, AdminPeriod{Days: p.Days, Price: p.Price})
	}
	return out
}

func (a *adminAPI) tariffsList(_ http.ResponseWriter, r *http.Request, ad adminCtx) (any, error) {
	all, err := a.d.Core.AllTariffs(r.Context(), ad.Admin.TgID)
	if err != nil {
		return nil, err
	}
	out := AdminTariffList{Tariffs: make([]AdminTariff, 0, len(all)), Squads: []AdminSquad{}, SquadsOK: true}
	for _, t := range all {
		out.Tariffs = append(out.Tariffs, tariffDTO(t))
	}
	squads, err := a.d.Core.PanelSquads(r.Context(), ad.Admin.TgID)
	if errors.Is(err, core.ErrPanelDown) {
		out.SquadsOK = false
	} else if err != nil {
		return nil, err
	}
	for _, s := range squads {
		out.Squads = append(out.Squads, AdminSquad{UUID: s.UUID, Name: s.Name, Members: s.Members})
	}
	return out, nil
}

// tariffSave создаёт тариф (адрес без id) или меняет его. Уже проданные подписки не меняются.
func (a *adminAPI) tariffSave(w http.ResponseWriter, r *http.Request, ad adminCtx) (any, error) {
	var in AdminTariff
	if err := Decode(w, r, &in); err != nil {
		return nil, err
	}
	in.ID = 0
	if r.PathValue("id") != "" {
		id, err := pathID(r)
		if err != nil {
			return nil, err
		}
		in.ID = id
	}
	t := core.Tariff{ID: in.ID, Name: in.Name, TrafficBytes: in.TrafficBytes, Devices: in.Devices, Reset: in.Reset,
		Squads: in.Squads, Active: in.Active, Sort: in.Sort}
	for _, p := range in.Periods {
		t.Periods = append(t.Periods, core.Period{Days: p.Days, Price: p.Price})
	}
	saved, err := a.d.Core.SaveTariff(r.Context(), ad.Admin.TgID, t)
	if err != nil {
		return nil, err
	}
	return tariffDTO(saved), nil
}

// AdminPromo это промокод со ссылкой для бота.
type AdminPromo struct {
	ID        int64      `json:"id"`
	Code      string     `json:"code"`
	Kind      string     `json:"kind"` // money (Value в копейках) или days (Value в днях)
	Value     int64      `json:"value"`
	TariffID  int64      `json:"tariff_id"`
	MaxUses   int        `json:"max_uses"` // 0 без лимита
	Uses      int        `json:"uses"`
	NewOnly   bool       `json:"new_only"`
	ExpiresAt *time.Time `json:"expires_at"`
	Active    bool       `json:"active"`
	// Link это t.me/<бот>?start=promo_<код>. Пусто, пока бот не узнал своё имя.
	Link string `json:"link"`
}

// AdminPromoList это страница промокодов.
type AdminPromoList struct {
	Items []AdminPromo `json:"items"`
	Next  int64        `json:"next"`
}

func (a *adminAPI) promoDTO(p core.Promo) AdminPromo {
	link := ""
	if a.d.Bot != nil {
		if name := a.d.Bot(); name != "" {
			link = "https://t.me/" + name + "?start=promo_" + url.QueryEscape(p.Code)
		}
	}
	return AdminPromo{ID: p.ID, Code: p.Code, Kind: string(p.Kind), Value: p.Value, TariffID: p.TariffID, MaxUses: p.MaxUses,
		Uses: p.Uses, NewOnly: p.NewOnly, ExpiresAt: optTime(p.ExpiresAt), Active: p.Active, Link: link}
}

func (a *adminAPI) promosList(_ http.ResponseWriter, r *http.Request, ad adminCtx) (any, error) {
	cur, err := queryCursor(r)
	if err != nil {
		return nil, err
	}
	page, err := a.d.Core.Promos(r.Context(), ad.Admin.TgID, cur)
	if err != nil {
		return nil, err
	}
	out := AdminPromoList{Items: make([]AdminPromo, 0, len(page.Items)), Next: page.Next}
	for _, p := range page.Items {
		out.Items = append(out.Items, a.promoDTO(p))
	}
	return out, nil
}

// promoSave создаёт промокод (адрес без id) или меняет его. Число активаций правка не трогает.
func (a *adminAPI) promoSave(w http.ResponseWriter, r *http.Request, ad adminCtx) (any, error) {
	var in struct {
		Code      string     `json:"code"`
		Kind      string     `json:"kind"`
		Value     int64      `json:"value"`
		TariffID  int64      `json:"tariff_id"`
		MaxUses   int        `json:"max_uses"`
		NewOnly   bool       `json:"new_only"`
		ExpiresAt *time.Time `json:"expires_at"`
		Active    *bool      `json:"active"`
	}
	if err := Decode(w, r, &in); err != nil {
		return nil, err
	}
	p := core.Promo{Code: in.Code, Kind: core.PromoKind(in.Kind), Value: in.Value, TariffID: in.TariffID, MaxUses: in.MaxUses,
		NewOnly: in.NewOnly, Active: in.Active == nil || *in.Active}
	if in.ExpiresAt != nil {
		p.ExpiresAt = *in.ExpiresAt
	}
	if r.PathValue("id") != "" {
		id, err := pathID(r)
		if err != nil {
			return nil, err
		}
		p.ID = id
	}
	saved, err := a.d.Core.SavePromo(r.Context(), ad.Admin.TgID, p)
	if err != nil {
		return nil, err
	}
	// После правки число активаций берётся из базы: SavePromo его не знает.
	if p.ID != 0 {
		page, err := a.d.Core.Promos(r.Context(), ad.Admin.TgID, p.ID+1)
		if err == nil && len(page.Items) > 0 && page.Items[0].ID == p.ID {
			saved.Uses = page.Items[0].Uses
		}
	}
	return a.promoDTO(saved), nil
}

func (a *adminAPI) promoActive(w http.ResponseWriter, r *http.Request, ad adminCtx) (any, error) {
	id, err := pathID(r)
	if err != nil {
		return nil, err
	}
	var in struct {
		Active bool `json:"active"`
	}
	if err := Decode(w, r, &in); err != nil {
		return nil, err
	}
	p, err := a.d.Core.SetPromoActive(r.Context(), ad.Admin.TgID, id, in.Active)
	if err != nil {
		return nil, err
	}
	return a.promoDTO(p), nil
}
