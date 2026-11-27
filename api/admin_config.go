package api

import (
	"net/http"
	"strings"
	"time"

	"github.com/bini69-oi/worker/core"
)

// AdminText это текст бота для редактора: умолчание рядом с правкой и список подстановок.
type AdminText struct {
	Key       string   `json:"key"`
	Params    []string `json:"params"`
	DefaultRu string   `json:"default_ru"`
	DefaultEn string   `json:"default_en"`
	Ru        string   `json:"ru"`
	En        string   `json:"en"`
	ChangedRu bool     `json:"changed_ru"`
	ChangedEn bool     `json:"changed_en"`
}

// AdminTextList это тексты, подходящие под поиск.
type AdminTextList struct {
	Items []AdminText `json:"items"`
}

// AdminSetting это настройка для экрана. У секрета в value только маска.
type AdminSetting struct {
	Key     string `json:"key"`
	Kind    string `json:"kind"`
	Group   string `json:"group"`
	Desc    string `json:"desc"`
	Default string `json:"default"`
	Value   string `json:"value"`
	Changed bool   `json:"changed"`
	Secret  bool   `json:"secret"`
	Confirm bool   `json:"confirm"` // меняется только через «Да» владельца в Telegram
}

// AdminSettingList это настройки, подходящие под поиск.
type AdminSettingList struct {
	Items []AdminSetting `json:"items"`
}

// AdminSettingResult это итог правки: pending значит, что ждём «Да» владельца, до него действует старое значение.
type AdminSettingResult struct {
	Pending   bool  `json:"pending"`
	ConfirmID int64 `json:"confirm_id"`
}

// AdminGateway это касса: состояние и её настройки (ключи маской).
type AdminGateway struct {
	Name       string         `json:"name"`
	Title      string         `json:"title"`
	Enabled    bool           `json:"enabled"`
	Configured bool           `json:"configured"`
	Sort       int64          `json:"sort"`
	LastPaidAt *time.Time     `json:"last_paid_at"`
	Pending    int            `json:"pending"`
	Settings   []AdminSetting `json:"settings"`
}

// AdminGatewayList это все кассы.
type AdminGatewayList struct {
	Items []AdminGateway `json:"items"`
}

func (a *adminAPI) textsList(_ http.ResponseWriter, r *http.Request, ad adminCtx) (any, error) {
	list, err := a.d.Core.TextList(r.Context(), ad.Admin.TgID)
	if err != nil {
		return nil, err
	}
	q := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	out := AdminTextList{Items: []AdminText{}}
	for _, t := range list {
		if q != "" && !strings.Contains(strings.ToLower(t.Key), q) && !strings.Contains(strings.ToLower(t.Value["ru"]), q) {
			continue
		}
		out.Items = append(out.Items, AdminText{Key: t.Key, Params: t.Params, DefaultRu: t.Default["ru"], DefaultEn: t.Default["en"],
			Ru: t.Value["ru"], En: t.Value["en"], ChangedRu: t.Changed["ru"], ChangedEn: t.Changed["en"]})
	}
	return out, nil
}

func (a *adminAPI) textSet(w http.ResponseWriter, r *http.Request, ad adminCtx) (any, error) {
	var in struct {
		Key   string `json:"key"`
		Lang  string `json:"lang"`
		Value string `json:"value"`
	}
	if err := Decode(w, r, &in); err != nil {
		return nil, err
	}
	return nil, a.d.Core.SetText(r.Context(), ad.Admin.TgID, in.Key, in.Lang, in.Value)
}

func (a *adminAPI) textReset(w http.ResponseWriter, r *http.Request, ad adminCtx) (any, error) {
	var in struct {
		Key  string `json:"key"`
		Lang string `json:"lang"`
	}
	if err := Decode(w, r, &in); err != nil {
		return nil, err
	}
	return nil, a.d.Core.ResetText(r.Context(), ad.Admin.TgID, in.Key, in.Lang)
}

func settingDTO(s core.SettingInfo) AdminSetting {
	return AdminSetting{Key: s.Key, Kind: s.Kind, Group: s.Group, Desc: s.Desc, Default: s.Default, Value: s.Value,
		Changed: s.Changed, Secret: s.Secret, Confirm: s.Confirm}
}

func (a *adminAPI) settingsList(_ http.ResponseWriter, r *http.Request, ad adminCtx) (any, error) {
	list, err := a.d.Core.SettingList(r.Context(), ad.Admin.TgID)
	if err != nil {
		return nil, err
	}
	q := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	group := r.URL.Query().Get("group")
	out := AdminSettingList{Items: []AdminSetting{}}
	for _, s := range list {
		if (group != "" && s.Group != group) ||
			(q != "" && !strings.Contains(strings.ToLower(s.Key), q) && !strings.Contains(strings.ToLower(s.Desc), q)) {
			continue
		}
		out.Items = append(out.Items, settingDTO(s))
	}
	return out, nil
}

func settingResult(res core.SettingResult, err error) (any, error) {
	return AdminSettingResult{Pending: res.Pending, ConfirmID: res.ConfirmID}, err
}

func (a *adminAPI) settingSet(w http.ResponseWriter, r *http.Request, ad adminCtx) (any, error) {
	var in struct {
		Key   string `json:"key"`
		Value string `json:"value"`
	}
	if err := Decode(w, r, &in); err != nil {
		return nil, err
	}
	return settingResult(a.d.Core.SetSetting(r.Context(), ad.Admin.TgID, in.Key, in.Value))
}

func (a *adminAPI) settingReset(w http.ResponseWriter, r *http.Request, ad adminCtx) (any, error) {
	var in struct {
		Key string `json:"key"`
	}
	if err := Decode(w, r, &in); err != nil {
		return nil, err
	}
	return settingResult(a.d.Core.ResetSetting(r.Context(), ad.Admin.TgID, in.Key))
}

func (a *adminAPI) gatewaysList(_ http.ResponseWriter, r *http.Request, ad adminCtx) (any, error) {
	states, err := a.d.Core.GatewayStates(r.Context(), ad.Admin.TgID)
	if err != nil {
		return nil, err
	}
	all, err := a.d.Core.SettingList(r.Context(), ad.Admin.TgID)
	if err != nil {
		return nil, err
	}
	out := AdminGatewayList{Items: make([]AdminGateway, 0, len(states))}
	for _, g := range states {
		item := AdminGateway{Name: g.Name, Title: g.Title, Enabled: g.Enabled, Configured: g.Configured, Sort: g.Sort,
			LastPaidAt: optTime(g.LastPaidAt), Pending: g.Pending, Settings: []AdminSetting{}}
		for _, s := range all {
			if strings.HasPrefix(s.Key, g.Name+".") {
				item.Settings = append(item.Settings, settingDTO(s))
			}
		}
		out.Items = append(out.Items, item)
	}
	return out, nil
}

// AdminMaintenance это техработы: идут ли и какой текст видят абоненты.
type AdminMaintenance struct {
	On   bool   `json:"on"`
	Text string `json:"text"`
}

func (a *adminAPI) maintenanceGet(_ http.ResponseWriter, r *http.Request, ad adminCtx) (any, error) {
	m, err := a.d.Core.MaintenanceState(r.Context(), ad.Admin.TgID)
	return AdminMaintenance{On: m.On, Text: m.Text}, err
}

func (a *adminAPI) maintenanceSet(w http.ResponseWriter, r *http.Request, ad adminCtx) (any, error) {
	var in AdminMaintenance
	if err := Decode(w, r, &in); err != nil {
		return nil, err
	}
	if err := a.d.Core.SetMaintenance(r.Context(), ad.Admin.TgID, in.On, in.Text); err != nil {
		return nil, err
	}
	return a.maintenanceGet(w, r, ad)
}
