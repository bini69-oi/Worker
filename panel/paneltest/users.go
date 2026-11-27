package paneltest

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"
)

type userBody struct {
	UUID        string   `json:"uuid"`
	ID          int64    `json:"id"`
	Username    string   `json:"username"`
	Status      string   `json:"status"`
	ExpireAt    string   `json:"expireAt"`
	TelegramID  int64    `json:"telegramId"`
	TrafficMax  int64    `json:"trafficLimitBytes"`
	Strategy    string   `json:"trafficLimitStrategy"`
	DeviceLimit int64    `json:"hwidDeviceLimit"`
	Squads      []string `json:"activeInternalSquads"`
	Description *string  `json:"description"` // нет в запросе: панель оставляет прежнее
}

// view это абонент в виде ответа панели своего поколения.
func (p *Panel) view(u *User) map[string]any {
	squads := make([]any, 0, len(u.Squads))
	for _, s := range u.Squads {
		if p.gen == V3 {
			squads = append(squads, map[string]any{"uuid": s, "name": "squad"})
		} else {
			squads = append(squads, s)
		}
	}
	out := map[string]any{
		"username":             u.Username,
		"status":               u.Status,
		"expireAt":             u.ExpireAt.UTC().Format("2006-01-02T15:04:05.000Z"),
		"shortUuid":            u.ShortUUID,
		"subscriptionUrl":      p.srv.URL + "/sub/" + u.ShortUUID,
		"telegramId":           u.TelegramID,
		"trafficLimitBytes":    u.TrafficMax,
		"trafficLimitStrategy": u.Strategy,
		"hwidDeviceLimit":      u.DeviceLimit,
		"activeInternalSquads": squads,
		"description":          u.Description,
		"createdAt":            u.CreatedAt.UTC().Format("2006-01-02T15:04:05.000Z"),
	}
	if p.gen == V3 {
		out["id"] = u.ID
	} else {
		out["uuid"] = u.UUID
	}
	return out
}

// find ищет абонента по идентификатору этого поколения.
func (p *Panel) find(ref string) *User {
	for _, u := range p.users {
		if p.refOf(u) == ref {
			return u
		}
	}
	return nil
}

func (p *Panel) findName(name string) *User {
	for _, u := range p.users {
		if u.Username == name {
			return u
		}
	}
	return nil
}

// apply переносит поля запроса в абонента. Ошибка это текст отказа панели.
func (p *Panel) apply(u *User, b userBody) string {
	exp, err := time.Parse(time.RFC3339, b.ExpireAt)
	if err != nil {
		return "expireAt is not a date"
	}
	if p.gen == V3 && !exp.After(p.now()) {
		return "expireAt must be in the future"
	}
	if b.Status != "ACTIVE" && b.Status != "DISABLED" {
		return "bad status"
	}
	u.Username, u.Status, u.ExpireAt = b.Username, b.Status, exp
	u.TelegramID, u.TrafficMax, u.Strategy = b.TelegramID, b.TrafficMax, b.Strategy
	u.DeviceLimit = b.DeviceLimit
	if b.Description != nil {
		u.Description = *b.Description
	}
	if b.Squads != nil {
		u.Squads = b.Squads
	}
	return ""
}

// listUsers отдаёт страницу абонентов по start и size, как GET /api/users.
func (p *Panel) listUsers(w http.ResponseWriter, r *http.Request) {
	start, err1 := strconv.Atoi(r.URL.Query().Get("start"))
	size, err2 := strconv.Atoi(r.URL.Query().Get("size"))
	if err1 != nil || err2 != nil || start < 0 || size < 1 {
		fail(w, http.StatusBadRequest, "bad start or size")
		return
	}
	page := []map[string]any{}
	for i := start; i < len(p.users) && i < start+size; i++ {
		page = append(page, p.view(p.users[i]))
	}
	reply(w, map[string]any{"users": page, "total": len(p.users)})
}

func (p *Panel) createUser(w http.ResponseWriter, r *http.Request) {
	var b userBody
	if json.NewDecoder(r.Body).Decode(&b) != nil || b.Username == "" {
		fail(w, http.StatusBadRequest, "username is required")
		return
	}
	if p.findName(b.Username) != nil {
		fail(w, http.StatusBadRequest, "user already exists")
		return
	}
	u := &User{UUID: newUUID(), ID: p.nextID, ShortUUID: newShort(), CreatedAt: p.now()}
	if msg := p.apply(u, b); msg != "" {
		fail(w, http.StatusBadRequest, msg)
		return
	}
	p.nextID++
	p.users = append(p.users, u)
	reply(w, p.view(u))
}

func (p *Panel) patchUser(w http.ResponseWriter, r *http.Request) {
	var b userBody
	if json.NewDecoder(r.Body).Decode(&b) != nil {
		fail(w, http.StatusBadRequest, "bad json")
		return
	}
	var u *User
	if p.gen == V3 && b.ID > 0 {
		u = p.find(itoa(b.ID))
	} else if p.gen == V2 && b.UUID != "" {
		u = p.find(b.UUID)
	}
	if u == nil {
		fail(w, http.StatusNotFound, "user not found")
		return
	}
	if msg := p.apply(u, b); msg != "" {
		fail(w, http.StatusBadRequest, msg)
		return
	}
	reply(w, p.view(u))
}

func (p *Panel) getUser(w http.ResponseWriter, ref string) {
	if u := p.find(ref); u != nil {
		reply(w, p.view(u))
		return
	}
	fail(w, http.StatusNotFound, "user not found")
}

// byUsername одинаковый в 2.x и 3.x.
func (p *Panel) byUsername(w http.ResponseWriter, name string) {
	if u := p.findName(name); u != nil {
		reply(w, p.view(u))
		return
	}
	fail(w, http.StatusNotFound, "user not found")
}

func (p *Panel) userAction(w http.ResponseWriter, ref, action string) {
	u := p.find(ref)
	if u == nil {
		fail(w, http.StatusNotFound, "user not found")
		return
	}
	switch action {
	case "revoke":
		u.ShortUUID = newShort()
	case "enable":
		u.Status = "ACTIVE"
	case "disable":
		u.Status = "DISABLED"
	default:
		fail(w, http.StatusNotFound, "unknown action")
		return
	}
	reply(w, p.view(u))
}
