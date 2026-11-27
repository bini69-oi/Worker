package panel

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// MaxPageSize это наибольшая страница списка пользователей, ограничение клиента.
const MaxPageSize = 500

// MonthRolling это скользящий месяц, стратегия сброса трафика, которую знает панель.
const MonthRolling Strategy = "MONTH_ROLLING"

// ListedUser это пользователь панели, прошедший проверку. Только такие и отдаются.
type ListedUser struct {
	// PanelID это идентификатор для запросов к панели: uuid в 2.x, число в 3.x, строкой.
	PanelID string
	// UUID есть только у панели 2.x. В 3.x пользователя зовут по числу, поле пустое.
	UUID     string
	Username string
	// TelegramID равен нулю, если у пользователя в панели его нет.
	TelegramID int64
	ExpireAt   time.Time
	// Status это статус панели как есть: ACTIVE, DISABLED, LIMITED, EXPIRED.
	Status string
	// TrafficLimitBytes и DeviceLimit равны нулю, если лимита нет.
	TrafficLimitBytes int64
	DeviceLimit       int
	TrafficReset      Strategy
	// Squads это uuid внутренних сквадов.
	Squads    []string
	SubURL    string
	CreatedAt time.Time
}

// SkippedUser это запись панели, которую нельзя отдать как пользователя.
type SkippedUser struct {
	// Index это позиция записи в ответе панели на этой странице, с нуля.
	Index int
	// Username и PanelID заполнены, если их удалось прочитать и они безопасны для показа.
	Username string
	PanelID  string
	// Reason это причина для отчёта, без значений из ответа панели.
	Reason string
}

// UsersPage это одна страница списка пользователей.
type UsersPage struct {
	Users   []ListedUser
	Skipped []SkippedUser
	// Total это число пользователей в панели по её счёту, а не на странице.
	Total int
	// Fetched это сколько записей прислала панель, равно len(Users)+len(Skipped).
	// Нуль при total больше start значит, что список закончился раньше: читать дальше нечего.
	Fetched int
}

// ListUsers читает одну страницу пользователей: start это смещение, size от 1 до MaxPageSize.
// Только читает, панель не меняет. Мусорные записи попадают в Skipped, а не в Users, и не
// роняют страницу. Ответ без списка пользователей это ошибка.
func (c *Client) ListUsers(ctx context.Context, start, size int) (UsersPage, error) {
	if start < 0 || size < 1 || size > MaxPageSize {
		return UsersPage{}, errors.New("panel: список пользователей: start от 0, size от 1 до " + strconv.Itoa(MaxPageSize))
	}
	ct, err := c.current(ctx)
	if err != nil {
		return UsersPage{}, err
	}
	q := url.Values{"start": {strconv.Itoa(start)}, "size": {strconv.Itoa(size)}}
	var raw struct {
		Users json.RawMessage `json:"users"`
		Total json.RawMessage `json:"total"`
	}
	if err := c.do(ctx, http.MethodGet, "/api/users?"+q.Encode(), nil, &raw); err != nil {
		return UsersPage{}, err
	}
	var items []json.RawMessage
	if err := json.Unmarshal(raw.Users, &items); err != nil || raw.Users == nil {
		return UsersPage{}, errors.New("panel: GET /api/users: в ответе нет списка пользователей")
	}
	if len(items) > size {
		return UsersPage{}, errors.New("panel: GET /api/users: панель прислала больше записей, чем просили")
	}
	total, ok := nonNegInt(raw.Total)
	if !ok {
		return UsersPage{}, errors.New("panel: GET /api/users: в ответе нет числа пользователей")
	}
	page := UsersPage{Total: int(total), Fetched: len(items)}
	for i, item := range items {
		u, skip := ct.checkUser(item)
		if skip != nil {
			skip.Index = i
			page.Skipped = append(page.Skipped, *skip)
			continue
		}
		page.Users = append(page.Users, u)
	}
	return page, nil
}

// EachUsersPage читает всех пользователей страницами по size и отдаёт каждую в fn.
// Останавливается, когда прочитано total записей или панель прислала пустую страницу.
// Ошибка fn прерывает чтение и возвращается как есть.
func (c *Client) EachUsersPage(ctx context.Context, size int, fn func(UsersPage) error) error {
	for start := 0; ; {
		page, err := c.ListUsers(ctx, start, size)
		if err != nil {
			return err
		}
		if page.Fetched > 0 {
			if err := fn(page); err != nil {
				return err
			}
		}
		start += page.Fetched
		if page.Fetched == 0 || start >= page.Total {
			return nil
		}
	}
}
