package panel

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"
)

// Формат вебхука Remnawave (сверено с исходниками панели 3.4.17,
// src/queue/notifications/webhook-logger/webhook-logger.processor.ts):
//
//	X-Remnawave-Signature: hex(HMAC-SHA256(ключ = WEBHOOK_SECRET_HEADER, данные = тело как есть))
//	X-Remnawave-Timestamp: время события в ISO 8601, то же, что поле timestamp в теле
//
// Тело: {"scope","event","timestamp","data",...}. Для scope "user" в data лежит абонент.

const (
	// HeaderSignature и HeaderTimestamp это заголовки вебхука панели.
	HeaderSignature = "X-Remnawave-Signature"
	HeaderTimestamp = "X-Remnawave-Timestamp"

	// MaxWebhookBody ограничивает размер тела вебхука.
	MaxWebhookBody = 1 << 20
	// WebhookMaxAge это допустимый возраст метки времени.
	WebhookMaxAge = 5 * time.Minute
)

// Области событий (scope).
const (
	ScopeUser    = "user"
	ScopeNode    = "node"
	ScopeService = "service"
	ScopeErrors  = "errors"
	ScopeCRM     = "crm"
)

// Имена событий абонента, нужные ядру.
const (
	EventUserExpired      = "user.expired"
	EventUserLimited      = "user.limited"
	EventUserTrafficReset = "user.traffic_reset"
	EventUserModified     = "user.modified"
	EventUserDeleted      = "user.deleted"
	EventUserRevoked      = "user.revoked"
	EventUserFirstConnect = "user.first_connected"
)

// Ошибки проверки вебхука. Секрет в них не попадает.
var (
	ErrWebhookNoSecret     = errors.New("панель: секрет вебхука не задан")
	ErrWebhookNoSignature  = errors.New("панель: у вебхука нет подписи")
	ErrWebhookBadSignature = errors.New("панель: подпись вебхука не сошлась")
	ErrWebhookStale        = errors.New("панель: метка времени вебхука устарела или в будущем")
	ErrWebhookBadTimestamp = errors.New("панель: метка времени вебхука не читается")
	ErrWebhookTooLarge     = errors.New("панель: тело вебхука слишком большое")
	ErrWebhookRead         = errors.New("панель: тело вебхука не прочитано")
	ErrWebhookBadBody      = errors.New("панель: тело вебхука не разобрано")
)

// VerifyWebhook читает тело запроса и проверяет подпись и свежесть.
// Возвращает сырое тело, его потом отдают в ParseEvent.
func VerifyWebhook(r *http.Request, secret string, now time.Time) ([]byte, error) {
	if secret == "" {
		return nil, ErrWebhookNoSecret
	}
	got := strings.TrimSpace(r.Header.Get(HeaderSignature))
	if got == "" {
		return nil, ErrWebhookNoSignature
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, MaxWebhookBody+1))
	if err != nil {
		return nil, ErrWebhookRead
	}
	if len(body) > MaxWebhookBody {
		return nil, ErrWebhookTooLarge
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	want := mac.Sum(nil)
	gotBytes, err := hex.DecodeString(got)
	if err != nil || !hmac.Equal(gotBytes, want) {
		return nil, ErrWebhookBadSignature
	}
	// Время берётся из подписанного тела: заголовок можно убрать, а тело без ключа не поменять.
	// Без срока давности перехваченный вебхук можно было бы слать повторно бесконечно.
	var stamp struct {
		Timestamp time.Time `json:"timestamp"`
	}
	if json.Unmarshal(body, &stamp) != nil || stamp.Timestamp.IsZero() {
		return nil, ErrWebhookBadTimestamp
	}
	if d := now.Sub(stamp.Timestamp); d > WebhookMaxAge || d < -WebhookMaxAge {
		return nil, ErrWebhookStale
	}
	return body, nil
}

// EventUser это абонент из события, только поля, нужные ядру.
type EventUser struct {
	UUID              string
	ID                int64
	Username          string
	TelegramID        int64
	Status            string
	ExpireAt          time.Time
	UsedTrafficBytes  int64
	TrafficLimitBytes int64
}

// Event это разобранный вебхук. User заполнен только для области "user".
type Event struct {
	Scope string
	Name  string
	At    time.Time
	User  *EventUser
}

type wireEvent struct {
	Scope     string          `json:"scope"`
	Event     string          `json:"event"`
	Timestamp time.Time       `json:"timestamp"`
	Data      json.RawMessage `json:"data"`
}

type wireEventUser struct {
	wireUser
	UserTraffic struct {
		Used flexInt `json:"usedTrafficBytes"`
	} `json:"userTraffic"`
}

// ParseEvent разбирает тело вебхука. Неизвестные события не ошибка.
func ParseEvent(body []byte) (Event, error) {
	var w wireEvent
	if err := json.Unmarshal(body, &w); err != nil {
		return Event{}, ErrWebhookBadBody
	}
	if w.Scope == "" || w.Event == "" {
		return Event{}, ErrWebhookBadBody
	}
	ev := Event{Scope: w.Scope, Name: w.Event, At: w.Timestamp}
	if w.Scope != ScopeUser || len(w.Data) == 0 || string(w.Data) == "null" {
		return ev, nil
	}
	var u wireEventUser
	if err := json.Unmarshal(w.Data, &u); err != nil {
		return Event{}, ErrWebhookBadBody
	}
	ev.User = &EventUser{
		UUID:              u.UUID,
		ID:                int64(u.ID),
		Username:          u.Username,
		TelegramID:        int64(u.TelegramID),
		Status:            u.Status,
		ExpireAt:          u.ExpireAt.UTC(),
		UsedTrafficBytes:  int64(u.UserTraffic.Used),
		TrafficLimitBytes: int64(u.TrafficLimit),
	}
	return ev, nil
}
