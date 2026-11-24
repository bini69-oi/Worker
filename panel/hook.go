package panel

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/bini69-oi/worker/core"
)

// WebhookHandler принимает вебхуки панели: проверяет подпись секретом из настроек и передаёт событие ядру.
// Неверная подпись получает 401 и ничего не меняет.
func WebhookHandler(c *core.Core, secret func() string, log *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		body, err := VerifyWebhook(r, secret(), time.Now())
		if err != nil {
			log.Warn("вебхук панели отклонён", "err", err, "ip", r.RemoteAddr)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		ev, err := ParseEvent(body)
		if err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		ce := core.PanelEvent{Scope: ev.Scope, Name: ev.Name}
		if ev.User != nil {
			ce.TgID, ce.Username = ev.User.TelegramID, ev.User.Username
		}
		if err := c.HandlePanelEvent(r.Context(), ce); err != nil {
			log.Error("событие панели не обработано", "event", ev.Name, "err", err)
			http.Error(w, "retry later", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
}
