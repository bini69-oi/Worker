package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/bini69-oi/worker/core"
)

// AdminMember это владелец или помощник с правами.
type AdminMember struct {
	TgID     int64    `json:"tg_id"`
	Owner    bool     `json:"owner"`
	Perms    []string `json:"perms"`
	Disabled bool     `json:"disabled"`
}

// AdminTeam это команда.
type AdminTeam struct {
	Items []AdminMember `json:"items"`
}

// AdminSession это открытая сессия админки. Current это сессия того, кто смотрит.
type AdminSession struct {
	ID        string    `json:"id"`
	AdminTg   int64     `json:"admin_tg"`
	IP        string    `json:"ip"`
	Agent     string    `json:"agent"`
	CreatedAt time.Time `json:"created_at"`
	SeenAt    time.Time `json:"seen_at"`
	Current   bool      `json:"current"`
}

// AdminSessionList это открытые сессии всех админов.
type AdminSessionList struct {
	Items []AdminSession `json:"items"`
}

// AdminAuditList это страница журнала действий.
type AdminAuditList struct {
	Items []AdminAudit `json:"items"`
	Next  int64        `json:"next"`
}

func (a *adminAPI) teamList(_ http.ResponseWriter, r *http.Request, ad adminCtx) (any, error) {
	list, err := a.d.Core.Team(r.Context(), ad.Admin.TgID)
	if err != nil {
		return nil, err
	}
	out := AdminTeam{Items: make([]AdminMember, 0, len(list))}
	for _, m := range list {
		perms := make([]string, 0, len(m.Perms))
		for _, p := range m.Perms {
			perms = append(perms, string(p))
		}
		out.Items = append(out.Items, AdminMember{TgID: m.TgID, Owner: m.Owner, Perms: perms, Disabled: !m.DisabledAt.IsZero()})
	}
	return out, nil
}

func (a *adminAPI) teamAdd(w http.ResponseWriter, r *http.Request, ad adminCtx) (any, error) {
	var in struct {
		TgID     int64    `json:"tg_id"`
		Password string   `json:"password"`
		Perms    []string `json:"perms"`
	}
	if err := Decode(w, r, &in); err != nil {
		return nil, err
	}
	perms := make([]core.Perm, 0, len(in.Perms))
	for _, p := range in.Perms {
		perms = append(perms, core.Perm(p))
	}
	id, err := a.d.Core.RequestAddHelper(r.Context(), ad.Admin.TgID, in.TgID, perms, in.Password)
	return AdminSettingResult{Pending: true, ConfirmID: id}, err
}

func (a *adminAPI) teamRemove(w http.ResponseWriter, r *http.Request, ad adminCtx) (any, error) {
	var in struct {
		TgID int64 `json:"tg_id"`
	}
	if err := Decode(w, r, &in); err != nil {
		return nil, err
	}
	id, err := a.d.Core.RequestRemoveHelper(r.Context(), ad.Admin.TgID, in.TgID)
	return AdminSettingResult{Pending: true, ConfirmID: id}, err
}

func (a *adminAPI) sessionsList(_ http.ResponseWriter, r *http.Request, ad adminCtx) (any, error) {
	list, err := a.d.Core.Sessions(r.Context(), ad.Admin.TgID)
	if err != nil {
		return nil, err
	}
	out := AdminSessionList{Items: make([]AdminSession, 0, len(list))}
	for _, s := range list {
		out.Items = append(out.Items, AdminSession{ID: s.ID, AdminTg: s.AdminTg, IP: s.IP, Agent: s.Agent,
			CreatedAt: s.CreatedAt, SeenAt: s.SeenAt, Current: s.ID == ad.Session.ID})
	}
	return out, nil
}

func (a *adminAPI) sessionClose(w http.ResponseWriter, r *http.Request, ad adminCtx) (any, error) {
	var in struct {
		ID string `json:"id"`
	}
	if err := Decode(w, r, &in); err != nil {
		return nil, err
	}
	return nil, a.d.Core.CloseSession(r.Context(), ad.Admin.TgID, in.ID)
}

func (a *adminAPI) auditList(_ http.ResponseWriter, r *http.Request, ad adminCtx) (any, error) {
	cur, err := queryCursor(r)
	if err != nil {
		return nil, err
	}
	q := r.URL.Query()
	f := core.AuditFilter{Action: q.Get("action")}
	if raw := q.Get("admin"); raw != "" {
		if f.AdminTg, err = strconv.ParseInt(raw, 10, 64); err != nil {
			return nil, core.ErrInvalid
		}
	}
	if f.From, err = parseDay(q.Get("from")); err != nil {
		return nil, err
	}
	if f.To, err = parseDay(q.Get("to")); err != nil {
		return nil, err
	}
	if !f.To.IsZero() && len(q.Get("to")) == 10 {
		f.To = f.To.AddDate(0, 0, 1)
	}
	page, err := a.d.Core.Audit(r.Context(), ad.Admin.TgID, f, cur)
	if err != nil {
		return nil, err
	}
	out := AdminAuditList{Items: make([]AdminAudit, 0, len(page.Items)), Next: page.Next}
	for _, e := range page.Items {
		out.Items = append(out.Items, auditDTO(e))
	}
	return out, nil
}
