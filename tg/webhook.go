package tg

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/bini69-oi/worker/tgsign"
)

const maxWebhookBody = 1 << 20

// Urgent обрабатывает обновление сразу, до ответа Telegram. handled=false: обновление обычное, идёт в очередь.
// Ошибка даёт ответ 500, и Telegram пришлёт то же обновление снова. Так оплата не теряется,
// если обработка упала или процесс перезапустился с непустой очередью.
type Urgent func(ctx context.Context, u Update) (handled bool, err error)

// NewWebhook возвращает обработчик вебхука. Обновление кладётся в out без ожидания:
// если канал полон, отвечаем 503 и Telegram пришлёт то же обновление позже. urgent может быть nil.
func NewWebhook(secret string, out chan<- Update, urgent Urgent) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if tgsign.CheckWebhook(r, secret) != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var u Update
		body := http.MaxBytesReader(w, r.Body, maxWebhookBody)
		if err := json.NewDecoder(body).Decode(&u); err != nil {
			var tooBig *http.MaxBytesError
			if errors.As(err, &tooBig) {
				http.Error(w, "too large", http.StatusRequestEntityTooLarge)
				return
			}
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		if urgent != nil {
			handled, err := urgent(r.Context(), u)
			if handled {
				if err != nil {
					http.Error(w, "retry", http.StatusInternalServerError)
					return
				}
				w.WriteHeader(http.StatusOK)
				return
			}
		}
		select {
		case out <- u:
			w.WriteHeader(http.StatusOK)
		default:
			http.Error(w, "busy", http.StatusServiceUnavailable)
		}
	})
}
