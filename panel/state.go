package panel

import (
	"errors"
	"strconv"
	"strings"
	"time"
)

// Strategy говорит, как панель сбрасывает счётчик трафика.
type Strategy string

// Стратегии сброса трафика, как их называет панель.
const (
	NoReset Strategy = "NO_RESET"
	Daily   Strategy = "DAY"
	Weekly  Strategy = "WEEK"
	Monthly Strategy = "MONTH"
)

// State это итоговое состояние абонента, к которому панель надо привести.
// Оно берётся из базы Worker, поэтому повторная отправка безопасна.
type State struct {
	// PanelID это сохранённый идентификатор абонента в панели: uuid в 2.x,
	// число (id) в 3.x, строкой. Пустой, если абонента ещё не создавали.
	PanelID  string
	Username string
	// TelegramID равен нулю, если у абонента нет Telegram.
	TelegramID int64
	ExpireAt   time.Time
	// TrafficLimitBytes равен нулю, если трафик не ограничен.
	TrafficLimitBytes int64
	Strategy          Strategy
	// DeviceLimit равен нулю, если число устройств не ограничено.
	DeviceLimit int
	// Squads это uuid внутренних сквадов (групп серверов). Пустой список
	// значит «не трогать сквады в панели».
	Squads      []string
	Enabled     bool
	Description string
}

// Result это то, что панель знает об абоненте после синхронизации.
type Result struct {
	PanelID         string
	Username        string
	ShortUUID       string
	SubscriptionURL string
	ExpireAt        time.Time
	// Created истинно, если абонента создали при этом вызове.
	Created bool
}

// Device это устройство абонента, привязанное по HWID (идентификатор устройства).
type Device struct {
	HWID      string
	Platform  string
	Model     string
	CreatedAt time.Time
}

// Version это версия панели.
type Version struct {
	Raw   string
	Major int
	Minor int
}

// Generation возвращает 3 для панелей 3.x и более новых, иначе 2.
func (v Version) Generation() int {
	if v.Major >= 3 {
		return 3
	}
	return 2
}

func parseVersion(raw string) Version {
	v := Version{Raw: raw}
	parts := strings.Split(strings.TrimPrefix(strings.TrimSpace(raw), "v"), ".")
	if n, err := strconv.Atoi(parts[0]); err == nil {
		v.Major = n
	}
	if len(parts) > 1 {
		if n, err := strconv.Atoi(parts[1]); err == nil {
			v.Minor = n
		}
	}
	return v
}

func (s State) validate() error {
	if strings.TrimSpace(s.Username) == "" {
		return errors.New("panel: у состояния нет имени абонента")
	}
	if s.ExpireAt.IsZero() {
		return errors.New("panel: у состояния нет срока")
	}
	return nil
}
