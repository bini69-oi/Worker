package tg

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/bini69-oi/worker/tgsign"
)

const maxWebhookBody = 1 << 20

// NewWebhook возвращает обработчик вебхука. Обновление кладётся в out без ожидания:
// если канал полон, отвечаем 503 и Telegram пришлёт то же обновление позже.
func NewWebhook(secret string, out chan<- Update) http.Handler {
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
		select {
		case out <- u:
			w.WriteHeader(http.StatusOK)
		default:
			http.Error(w, "busy", http.StatusServiceUnavailable)
		}
	})
}
