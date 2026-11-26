package pay

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/bini69-oi/worker/core"
)

// Gateway это касса: общий набор действий ядра и проверка вебхука.
// Чтобы добавить кассу, достаточно одного файла с этим интерфейсом (см. docs/add-gateway.md).
type Gateway interface {
	core.Gateway
	// Verify проверяет подпись вебхука и разбирает его. Непроверенный вебхук возвращает ошибку и ничего не меняет.
	Verify(r *http.Request) (core.GatewayEvent, error)
}

// Acker это касса, которая ждёт в ответе на успешный вебхук своё слово (FreeKassa «YES», Robokassa «OK<номер>»).
// Без него касса считает уведомление недоставленным и повторяет его.
type Acker interface {
	Ack(ev core.GatewayEvent) string
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
		accept := func() {
			if a, is := g.(Acker); is {
				body := a.Ack(ev)
				ctype := "text/plain; charset=utf-8"
				if strings.HasPrefix(body, "{") { // CloudPayments, SeverPay и подобные ждут JSON
					ctype = "application/json"
				}
				w.Header().Set("Content-Type", ctype)
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(body))
				return
			}
			w.WriteHeader(http.StatusOK)
		}
		switch err := c.Credit(r.Context(), name, ev); {
		case err == nil:
			accept()
		case errors.Is(err, core.ErrNotFound):
			// Платёж не наш или уже удалён: отвечаем «принято», чтобы касса не повторяла вечно.
			log.Warn("вебхук о неизвестном платеже", "gateway", name, "external_id", ev.ExternalID)
			accept()
		case errors.Is(err, core.ErrAmountMismatch), errors.Is(err, core.ErrInvalid):
			// Сумма не та или номер кассы чужой: повтор не поможет, админ уже получил сообщение о сумме.
			log.Warn("оплата отклонена", "gateway", name, "err", err)
			http.Error(w, "rejected", http.StatusBadRequest)
		default:
			log.Error("оплата не зачислена", "gateway", name, "err", err)
			http.Error(w, "retry later", http.StatusInternalServerError)
		}
	})
	return mux
}

// ReturnURL это адрес, куда касса вернёт абонента после оплаты. Адрес из настройки главнее: в нём {payment}
// заменяется номером платежа. Пустая настройка: адрес по умолчанию от бота (диплинк paid_<номер>).
func ReturnURL(setting string, def func(paymentID int64) string, paymentID int64) string {
	id := strconv.FormatInt(paymentID, 10)
	if setting != "" {
		return strings.ReplaceAll(setting, "{payment}", id)
	}
	if def != nil {
		return def(paymentID)
	}
	return ""
}
