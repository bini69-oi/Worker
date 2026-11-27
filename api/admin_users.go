package api

import (
	"net/http"
	"time"

	"github.com/bini69-oi/worker/core"
)

// AdminUserRow это строка поиска абонентов.
type AdminUserRow struct {
	ID        int64      `json:"id"`
	TgID      int64      `json:"tg_id"`
	Username  string     `json:"username"`
	Status    string     `json:"status"` // none, trial, active, expired
	Banned    bool       `json:"banned"`
	Balance   int64      `json:"balance"` // копейки
	ExpiresAt *time.Time `json:"expires_at"`
	CreatedAt time.Time  `json:"created_at"`
}

// AdminUserList это страница поиска. Next это курсор следующей страницы, 0 значит конец.
type AdminUserList struct {
	Items []AdminUserRow `json:"items"`
	Next  int64          `json:"next"`
}

// AdminSub это подписка в карточке абонента.
type AdminSub struct {
	Tariff       string    `json:"tariff"` // пусто у пробного
	Trial        bool      `json:"trial"`
	Status       string    `json:"status"`
	PeriodDays   int       `json:"period_days"`
	Price        int64     `json:"price"`
	ExpiresAt    time.Time `json:"expires_at"`
	AutoRenew    bool      `json:"auto_renew"`
	TrafficBytes int64     `json:"traffic_bytes"` // 0 без лимита
	DeviceLimit  int       `json:"device_limit"`  // 0 без лимита

	// Ждущая смена тарифа: включится, когда кончится текущий период. Цена уже списана.
	NextTariff string `json:"next_tariff"` // пусто, если смены нет
	NextDays   int    `json:"next_days"`
	NextPrice  int64  `json:"next_price"`
	// Докупленное: трафик действует до конца периода, устройства переходят в следующий.
	ExtraTrafficBytes int64      `json:"extra_traffic_bytes"`
	ExtraTrafficUntil *time.Time `json:"extra_traffic_until"`
	ExtraDevices      int        `json:"extra_devices"`
	// Imported значит, что подписка перенесена из панели: лимиты берутся оттуда, а не из тарифа.
	Imported bool            `json:"imported"`
	Own      *AdminOwnLimits `json:"own"`
}

// AdminOwnLimits это лимиты и имя пользователя перенесённой подписки в панели.
type AdminOwnLimits struct {
	Username     string `json:"username"`
	TrafficBytes int64  `json:"traffic_bytes"`
	Devices      int    `json:"devices"`
	Reset        string `json:"reset"`
}

// AdminLedger это движение денег в карточке.
type AdminLedger struct {
	Amount    int64     `json:"amount"`
	Reason    string    `json:"reason"`
	Comment   string    `json:"comment"`
	AdminTg   int64     `json:"admin_tg"`
	CreatedAt time.Time `json:"created_at"`
}

// AdminPayment это платёж в списках админки.
type AdminPayment struct {
	ID        int64      `json:"id"`
	UserID    int64      `json:"user_id"`
	Username  string     `json:"username"`
	Gateway   string     `json:"gateway"`
	Amount    int64      `json:"amount"`
	Status    string     `json:"status"`
	CreatedAt time.Time  `json:"created_at"`
	PaidAt    *time.Time `json:"paid_at"`
}

// AdminAudit это строка журнала действий.
type AdminAudit struct {
	ID      int64             `json:"id"`
	AdminTg int64             `json:"admin_tg"`
	Action  string            `json:"action"`
	Target  string            `json:"target"`
	Details map[string]string `json:"details"`
	IP      string            `json:"ip"`
	At      time.Time         `json:"at"`
}

// AdminDevice это устройство абонента в панели.
type AdminDevice struct {
	HWID     string `json:"hwid"`
	Platform string `json:"platform"`
	Model    string `json:"model"`
}

// AdminUserCard это карточка абонента.
type AdminUserCard struct {
	ID          int64          `json:"id"`
	TgID        int64          `json:"tg_id"`
	Username    string         `json:"username"`
	Lang        string         `json:"lang"`
	Balance     int64          `json:"balance"`
	Banned      bool           `json:"banned"`
	Unreachable bool           `json:"unreachable"`
	CreatedAt   time.Time      `json:"created_at"`
	ReferrerTg  int64          `json:"referrer_tg"` // 0, если пришёл сам
	Sub         *AdminSub      `json:"sub"`
	Ledger      []AdminLedger  `json:"ledger"`
	Payments    []AdminPayment `json:"payments"`
	Audit       []AdminAudit   `json:"audit"`
	Devices     []AdminDevice  `json:"devices"`
	// DevicesOK ложно, если панель не ответила: фронт пишет «Панель не ответила».
	DevicesOK bool `json:"devices_ok"`
}

func (a *adminAPI) usersList(_ http.ResponseWriter, r *http.Request, ad adminCtx) (any, error) {
	cur, err := queryCursor(r)
	if err != nil {
		return nil, err
	}
	q := r.URL.Query()
	page, err := a.d.Core.FindUsers(r.Context(), ad.Admin.TgID, q.Get("q"), q.Get("filter"), cur)
	if err != nil {
		return nil, err
	}
	out := AdminUserList{Items: make([]AdminUserRow, 0, len(page.Items)), Next: page.Next}
	for _, row := range page.Items {
		u := row.User
		out.Items = append(out.Items, AdminUserRow{ID: u.ID, TgID: u.TgID, Username: u.Username, Status: row.Status,
			Banned: !u.BannedAt.IsZero(), Balance: u.Balance, ExpiresAt: optTime(row.ExpiresAt), CreatedAt: u.CreatedAt})
	}
	return out, nil
}

func (a *adminAPI) userCard(_ http.ResponseWriter, r *http.Request, ad adminCtx) (any, error) {
	id, err := pathID(r)
	if err != nil {
		return nil, err
	}
	c, err := a.d.Core.UserCard(r.Context(), ad.Admin.TgID, id)
	if err != nil {
		return nil, err
	}
	u := c.User
	out := AdminUserCard{ID: u.ID, TgID: u.TgID, Username: u.Username, Lang: u.Lang, Balance: u.Balance,
		Banned: !u.BannedAt.IsZero(), Unreachable: !u.UnreachableAt.IsZero(), CreatedAt: u.CreatedAt,
		Ledger: []AdminLedger{}, Payments: []AdminPayment{}, Audit: []AdminAudit{}, Devices: []AdminDevice{}, DevicesOK: c.DevicesOK}
	if c.Referrer != nil {
		out.ReferrerTg = c.Referrer.TgID
	}
	if s := c.Sub; s != nil {
		out.Sub = &AdminSub{Tariff: c.Tariff, Trial: s.Trial, Status: string(s.Status), PeriodDays: s.PeriodDays, Price: s.Price,
			ExpiresAt: s.ExpiresAt, AutoRenew: s.AutoRenew, TrafficBytes: c.TrafficBytes, DeviceLimit: c.DeviceLimit,
			NextTariff: c.NextTariff, NextDays: s.NextDays, NextPrice: s.NextPrice, ExtraTrafficBytes: s.ExtraTrafficBytes,
			ExtraTrafficUntil: optTime(s.ExtraTrafficUntil), ExtraDevices: s.ExtraDevices, Imported: s.Imported}
		if s.Imported {
			out.Sub.Own = &AdminOwnLimits{Username: s.Own.Username, TrafficBytes: s.Own.TrafficBytes, Devices: s.Own.Devices, Reset: s.Own.Reset}
		}
	}
	for _, e := range c.Ledger {
		out.Ledger = append(out.Ledger, AdminLedger{Amount: e.Amount, Reason: string(e.Reason), Comment: e.Comment,
			AdminTg: e.AdminID, CreatedAt: e.CreatedAt})
	}
	for _, p := range c.Payments {
		out.Payments = append(out.Payments, paymentDTO(p, u.Username))
	}
	for _, e := range c.Audit {
		out.Audit = append(out.Audit, auditDTO(e))
	}
	for _, d := range c.Devices {
		out.Devices = append(out.Devices, AdminDevice{HWID: d.HWID, Platform: d.Platform, Model: d.Model})
	}
	return out, nil
}

func paymentDTO(p core.Payment, username string) AdminPayment {
	return AdminPayment{ID: p.ID, UserID: p.UserID, Username: username, Gateway: p.Gateway, Amount: p.Amount,
		Status: string(p.Status), CreatedAt: p.CreatedAt, PaidAt: optTime(p.PaidAt)}
}

func auditDTO(e core.AuditRow) AdminAudit {
	d := e.Details
	if d == nil {
		d = map[string]string{}
	}
	return AdminAudit{ID: e.ID, AdminTg: e.AdminTg, Action: e.Action, Target: e.Target, Details: d, IP: e.IP, At: e.At}
}

// AdminBalance это баланс абонента после правки, копейки.
type AdminBalance struct {
	Balance int64 `json:"balance"`
}

func (a *adminAPI) userBalance(w http.ResponseWriter, r *http.Request, ad adminCtx) (any, error) {
	id, err := pathID(r)
	if err != nil {
		return nil, err
	}
	var in struct {
		Amount  int64  `json:"amount"`
		Comment string `json:"comment"`
	}
	if err := Decode(w, r, &in); err != nil {
		return nil, err
	}
	if _, err := a.d.Core.AdminAdjust(r.Context(), ad.Admin.TgID, id, in.Amount, in.Comment); err != nil {
		return nil, err
	}
	u, err := a.d.Core.User(r.Context(), id)
	return AdminBalance{Balance: u.Balance}, err
}

// AdminExpires это срок подписки после выдачи дней.
type AdminExpires struct {
	ExpiresAt time.Time `json:"expires_at"`
}

func (a *adminAPI) userDays(w http.ResponseWriter, r *http.Request, ad adminCtx) (any, error) {
	id, err := pathID(r)
	if err != nil {
		return nil, err
	}
	var in struct {
		Days int `json:"days"`
	}
	if err := Decode(w, r, &in); err != nil {
		return nil, err
	}
	sub, err := a.d.Core.GrantDays(r.Context(), ad.Admin.TgID, id, in.Days)
	return AdminExpires{ExpiresAt: sub.ExpiresAt}, err
}

func (a *adminAPI) userBan(on bool) adminHandler {
	return func(w http.ResponseWriter, r *http.Request, ad adminCtx) (any, error) {
		id, err := pathID(r)
		if err != nil {
			return nil, err
		}
		var in struct {
			Comment string `json:"comment"`
		}
		if err := Decode(w, r, &in); err != nil {
			return nil, err
		}
		if on {
			return nil, a.d.Core.Ban(r.Context(), ad.Admin.TgID, id, in.Comment)
		}
		return nil, a.d.Core.Unban(r.Context(), ad.Admin.TgID, id, in.Comment)
	}
}

func (a *adminAPI) userMessage(w http.ResponseWriter, r *http.Request, ad adminCtx) (any, error) {
	id, err := pathID(r)
	if err != nil {
		return nil, err
	}
	var in struct {
		Text string `json:"text"`
	}
	if err := Decode(w, r, &in); err != nil {
		return nil, err
	}
	return nil, a.d.Core.SendToUser(r.Context(), ad.Admin.TgID, id, in.Text)
}

func (a *adminAPI) userDevicesReset(_ http.ResponseWriter, r *http.Request, ad adminCtx) (any, error) {
	id, err := pathID(r)
	if err != nil {
		return nil, err
	}
	return nil, a.d.Core.AdminResetDevices(r.Context(), ad.Admin.TgID, id)
}

func (a *adminAPI) userLinkRevoke(_ http.ResponseWriter, r *http.Request, ad adminCtx) (any, error) {
	id, err := pathID(r)
	if err != nil {
		return nil, err
	}
	return nil, a.d.Core.AdminRevokeLink(r.Context(), ad.Admin.TgID, id)
}
