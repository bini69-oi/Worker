package panel

import (
	"bytes"
	"encoding/json"
	"math"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	maxFieldLen = 256
	maxSquads   = 256
	maxDevices  = 1 << 20
	// 2^53: до этого числа число из JSON читается без потери точности.
	maxExactInt = 1 << 53
)

var (
	uuidRe   = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
	statusRe = regexp.MustCompile(`^[A-Z_]{1,32}$`)
)

// rawUser это пользователь панели, каждое поле сырое: плохое поле не должно ронять разбор других.
type rawUser struct {
	UUID         json.RawMessage `json:"uuid"`
	ID           json.RawMessage `json:"id"`
	Username     json.RawMessage `json:"username"`
	Status       json.RawMessage `json:"status"`
	ExpireAt     json.RawMessage `json:"expireAt"`
	CreatedAt    json.RawMessage `json:"createdAt"`
	SubURL       json.RawMessage `json:"subscriptionUrl"`
	TelegramID   json.RawMessage `json:"telegramId"`
	TrafficLimit json.RawMessage `json:"trafficLimitBytes"`
	Strategy     json.RawMessage `json:"trafficLimitStrategy"`
	DeviceLimit  json.RawMessage `json:"hwidDeviceLimit"`
	Squads       json.RawMessage `json:"activeInternalSquads"`
}

// checkUser проверяет одну запись панели. Вторым значением возвращает причину пропуска.
func (c contract) checkUser(item json.RawMessage) (ListedUser, *SkippedUser) {
	var r rawUser
	if err := json.Unmarshal(item, &r); err != nil {
		return ListedUser{}, &SkippedUser{Reason: "запись панели не объект"}
	}
	var u ListedUser
	skip := &SkippedUser{}
	if s, ok := safeText(r.Username); ok {
		u.Username, skip.Username = s, s
	}
	if err := c.checkID(&u, r); err != "" {
		skip.Reason = err
		return ListedUser{}, skip
	}
	skip.PanelID = u.PanelID
	if reason := checkFields(&u, r); reason != "" {
		skip.Reason = reason
		return ListedUser{}, skip
	}
	return u, nil
}

// checkID проверяет идентификатор своего поколения панели: uuid в 2.x, число в 3.x.
func (c contract) checkID(u *ListedUser, r rawUser) string {
	if c.v3 {
		n, ok := nonNegInt(r.ID)
		if !ok || n == 0 {
			return "плохой id"
		}
		u.PanelID = strconv.FormatInt(n, 10)
		return ""
	}
	s, ok := safeText(r.UUID)
	if !ok || !uuidRe.MatchString(s) {
		return "плохой uuid"
	}
	u.UUID, u.PanelID = strings.ToLower(s), strings.ToLower(s)
	return ""
}

func checkFields(u *ListedUser, r rawUser) string {
	var ok bool
	if u.Username == "" {
		return "плохое имя"
	}
	status, good := safeText(r.Status)
	if !good || !statusRe.MatchString(status) {
		return "плохой статус"
	}
	u.Status = status
	if u.ExpireAt, ok = dateOf(r.ExpireAt); !ok {
		return "плохая дата окончания"
	}
	if u.CreatedAt, ok = dateOf(r.CreatedAt); !ok {
		return "плохая дата создания"
	}
	if u.TelegramID, ok = optInt(r.TelegramID); !ok {
		return "плохой Telegram ID"
	}
	if u.TrafficLimitBytes, ok = optInt(r.TrafficLimit); !ok {
		return "плохой лимит трафика"
	}
	dev, ok := optInt(r.DeviceLimit)
	if !ok || dev > maxDevices {
		return "плохой лимит устройств"
	}
	u.DeviceLimit = int(dev)
	if u.TrafficReset, ok = strategyOf(r.Strategy); !ok {
		return "плохая стратегия сброса"
	}
	var reason string
	if u.Squads, reason = squadsOf(r.Squads); reason != "" {
		return reason
	}
	if u.SubURL, ok = subURLOf(r.SubURL); !ok {
		return "плохая ссылка подписки"
	}
	return ""
}

// safeText читает строку JSON: непустая, до maxFieldLen символов, валидный UTF-8, без управляющих знаков.
func safeText(raw json.RawMessage) (string, bool) {
	var s string
	if json.Unmarshal(raw, &s) != nil || s == "" || !utf8.ValidString(s) || utf8.RuneCountInString(s) > maxFieldLen {
		return "", false
	}
	for _, r := range s {
		if unicode.IsControl(r) {
			return "", false
		}
	}
	return s, true
}

// nonNegInt читает целое не меньше нуля, числом или строкой. null и пустое это не число.
func nonNegInt(raw json.RawMessage) (int64, bool) {
	text := strings.Trim(string(bytes.TrimSpace(raw)), `"`)
	if text == "" || text == "null" {
		return 0, false
	}
	f, err := strconv.ParseFloat(text, 64)
	if err != nil || math.IsNaN(f) || f < 0 || f > maxExactInt || f != math.Trunc(f) {
		return 0, false
	}
	return int64(f), true
}

// optInt это nonNegInt, где отсутствие поля или null значит ноль («нет лимита»).
func optInt(raw json.RawMessage) (int64, bool) {
	if t := string(bytes.TrimSpace(raw)); t == "" || t == "null" {
		return 0, true
	}
	return nonNegInt(raw)
}

func dateOf(raw json.RawMessage) (time.Time, bool) {
	s, ok := safeText(raw)
	if !ok {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil || t.IsZero() || t.Year() < 1970 || t.Year() > 9999 {
		return time.Time{}, false
	}
	return t.UTC(), true
}

func strategyOf(raw json.RawMessage) (Strategy, bool) {
	s, ok := safeText(raw)
	if !ok {
		return "", false
	}
	switch st := Strategy(s); st {
	case NoReset, Daily, Weekly, Monthly, MonthRolling:
		return st, true
	}
	return "", false
}

// squadsOf читает сквады: список uuid строками или объектами с полем uuid (панель 3.x шлёт объекты).
func squadsOf(raw json.RawMessage) ([]string, string) {
	if t := string(bytes.TrimSpace(raw)); t == "" || t == "null" {
		return nil, ""
	}
	var items []json.RawMessage
	if json.Unmarshal(raw, &items) != nil || len(items) > maxSquads {
		return nil, "плохой список сквадов"
	}
	out := make([]string, 0, len(items))
	for _, it := range items {
		var obj struct {
			UUID json.RawMessage `json:"uuid"`
		}
		src := it
		if json.Unmarshal(it, &obj) == nil && obj.UUID != nil {
			src = obj.UUID
		}
		s, ok := safeText(src)
		if !ok || !uuidRe.MatchString(s) {
			return nil, "плохой uuid сквада"
		}
		out = append(out, strings.ToLower(s))
	}
	return out, ""
}

func subURLOf(raw json.RawMessage) (string, bool) {
	s, ok := safeText(raw)
	if !ok {
		return "", false
	}
	u, err := url.Parse(s)
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
		return "", false
	}
	return s, true
}
