package pay

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/bini69-oi/worker/core"
)

// Gateway это касса: общий набор действий ядра и проверка вебхука.
// Чтобы добавить кассу, достаточно одного файла с этим интерфейсом (см. docs/add-gateway.md).
type Gateway interface {
	core.Gateway
	// Verify проверяет подпись вебхука и разбирает его. Непроверенный вебхук возвращает ошибку и ничего не меняет.
	Verify(r *http.Request) (core.GatewayEvent, error)
}

// ErrNoWebhook отвечает касса, которая не принимает вебхуки (например Stars: оплата приходит через бота).
var ErrNoWebhook = errors.New("касса не принимает вебхуки")

// ErrBadSignature это общий ответ на поддельный вебхук.
var ErrBadSignature = errors.New("подпись вебхука не сходится")

// CoreGateways переводит список касс в список для ядра.
func CoreGateways(list []Gateway) []core.Gateway {
	out := make([]core.Gateway, len(list))
	for i, g := range list {
		out[i] = g
	}
	return out
}

// Handler принимает вебхуки всех касс по адресу POST /pay/{name}.
// Касса неизвестна: 404. Подпись не сошлась: 401. Оплата зачислена или уже была зачислена: 200.
func Handler(c *core.Core, list []Gateway, log *slog.Logger) http.Handler {
	byName := map[string]Gateway{}
	for _, g := range list {
		byName[g.Name()] = g
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /pay/{name}", func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		g, ok := byName[name]
		if !ok {
			http.NotFound(w, r)
			return
		}
		ev, err := g.Verify(r)
		if err != nil {
			log.Warn("вебхук кассы отклонён", "gateway", name, "err", err, "ip", r.RemoteAddr)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		switch err := c.Credit(r.Context(), name, ev); {
		case err == nil:
			w.WriteHeader(http.StatusOK)
		case errors.Is(err, core.ErrNotFound):
			// Платёж не наш или уже удалён: отвечаем 200, чтобы касса не повторяла вечно.
			log.Warn("вебхук о неизвестном платеже", "gateway", name, "external_id", ev.ExternalID)
			w.WriteHeader(http.StatusOK)
		case errors.Is(err, core.ErrAmountMismatch):
			http.Error(w, "amount mismatch", http.StatusBadRequest)
		default:
			log.Error("оплата не зачислена", "gateway", name, "err", err)
			http.Error(w, "retry later", http.StatusInternalServerError)
		}
	})
	return mux
}
