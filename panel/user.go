package panel

import (
	"strconv"
	"strings"
	"time"
)

// flexInt читает число, которое панель может прислать и числом, и строкой.
type flexInt int64

func (f *flexInt) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	if s == "" || s == "null" {
		*f = 0
		return nil
	}
	n, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return err
	}
	*f = flexInt(n)
	return nil
}

// wireUser это абонент так, как его присылает панель (поля обеих версий).
type wireUser struct {
	UUID            string    `json:"uuid"`
	ID              flexInt   `json:"id"`
	Username        string    `json:"username"`
	Status          string    `json:"status"`
	ExpireAt        time.Time `json:"expireAt"`
	ShortUUID       string    `json:"shortUuid"`
	SubscriptionURL string    `json:"subscriptionUrl"`
	TelegramID      flexInt   `json:"telegramId"`
	TrafficLimit    flexInt   `json:"trafficLimitBytes"`
	Strategy        string    `json:"trafficLimitStrategy"`
	DeviceLimit     flexInt   `json:"hwidDeviceLimit"`
}

func (c contract) result(u wireUser, created bool) Result {
	return Result{
		PanelID:         c.refOf(u),
		Username:        u.Username,
		ShortUUID:       u.ShortUUID,
		SubscriptionURL: u.SubscriptionURL,
		ExpireAt:        u.ExpireAt.UTC(),
		// LIMITED значит, что кончился трафик, а не что админ выключил: абонент включён, сверке трогать нечего.
		Enabled: u.Status == "ACTIVE" || u.Status == "LIMITED",
		Created: created,
	}
}

// payload собирает тело создания или обновления абонента из состояния.
func payload(s State, c contract, now time.Time) map[string]any {
	status := "DISABLED"
	if s.Enabled {
		status = "ACTIVE"
	}
	strategy := s.Strategy
	if strategy == "" {
		strategy = NoReset
	}
	body := map[string]any{
		"username":             s.Username,
		"status":               status,
		"expireAt":             c.expireText(s.ExpireAt, now),
		"trafficLimitBytes":    s.TrafficLimitBytes,
		"trafficLimitStrategy": string(strategy),
		"hwidDeviceLimit":      s.DeviceLimit,
	}
	if len(s.Squads) > 0 {
		body["activeInternalSquads"] = s.Squads
	}
	if s.TelegramID != 0 {
		body["telegramId"] = s.TelegramID
	}
	if s.Description != "" {
		body["description"] = s.Description
	}
	return body
}
