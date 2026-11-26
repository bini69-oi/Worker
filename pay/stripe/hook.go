package stripe

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/bini69-oi/worker/core"
	"github.com/bini69-oi/worker/pay"
)

// tolerance это окно метки времени в подписи: старше или новее не принимаем (защита от повтора).
const tolerance = 5 * time.Minute

// event это вебхук Stripe: тип и объект внутри data.object.
type event struct {
	Type string `json:"type"`
	Data struct {
		Object json.RawMessage `json:"object"`
	} `json:"data"`
}

// charge это платёж картой в событии charge.refunded.
type charge struct {
	ID             string            `json:"id"`
	Amount         int64             `json:"amount"`
	AmountRefunded int64             `json:"amount_refunded"`
	Refunded       bool              `json:"refunded"`
	Metadata       map[string]string `json:"metadata"`
}

// Verify проверяет подпись заголовка Stripe-Signature и разбирает событие.
func (g *Gateway) Verify(r *http.Request) (core.GatewayEvent, error) {
	raw, err := io.ReadAll(io.LimitReader(r.Body, maxBody))
	if err != nil {
		return core.GatewayEvent{}, errors.New("вебхук Stripe не прочитан")
	}
	if !validSignature(raw, r.Header.Get("Stripe-Signature"), g.cfg.Secret(keyWebhook), g.now()) {
		return core.GatewayEvent{}, pay.ErrBadSignature
	}
	var ev event
	if err := json.Unmarshal(raw, &ev); err != nil {
		return core.GatewayEvent{}, errors.New("вебхук Stripe не разобран")
	}
	switch ev.Type {
	case "checkout.session.completed", "checkout.session.async_payment_succeeded",
		"checkout.session.async_payment_failed", "checkout.session.expired":
		return g.sessionEvent(ev)
	case "charge.refunded":
		return refundEvent(ev)
	}
	// Прочие события нам не нужны: ответ 200 без зачисления.
	return core.GatewayEvent{Status: core.PayPending}, nil
}

func (g *Gateway) sessionEvent(ev event) (core.GatewayEvent, error) {
	var s session
	if err := json.Unmarshal(ev.Data.Object, &s); err != nil || s.ID == "" {
		return core.GatewayEvent{}, errors.New("в вебхуке Stripe нет сессии")
	}
	out := core.GatewayEvent{PaymentID: paymentID(s.Metadata, s.ClientReferenceID), ExternalID: s.ID, Status: core.PayPending}
	switch ev.Type {
	case "checkout.session.completed":
		// Оплата картой приходит сразу paid, отложенные способы позже отдельным событием.
		if s.PaymentStatus == "paid" {
			out.Status = core.PayPaid
		}
	case "checkout.session.async_payment_succeeded":
		out.Status = core.PayPaid
	default:
		out.Status = core.PayFailed
	}
	return g.withAmount(out, s)
}

// refundEvent разбирает полный возврат. Частичный возврат оплату не отменяет.
// Номер платежа берём из metadata платежа (кладём при создании), номер сессии в charge не приходит.
func refundEvent(ev event) (core.GatewayEvent, error) {
	var c charge
	if err := json.Unmarshal(ev.Data.Object, &c); err != nil || c.ID == "" {
		return core.GatewayEvent{}, errors.New("в вебхуке Stripe нет платежа")
	}
	if !c.Refunded || c.AmountRefunded < c.Amount {
		return core.GatewayEvent{Status: core.PayPending}, nil
	}
	id := paymentID(c.Metadata, "")
	if id == 0 {
		return core.GatewayEvent{}, errors.New("в возврате Stripe нет нашего номера платежа")
	}
	return core.GatewayEvent{PaymentID: id, Status: core.PayRefunded}, nil
}

// validSignature сверяет заголовок вида "t=...,v1=...,v1=...": HMAC-SHA256 от "t.тело" секретом endpoint.
// Хватает любой из подписей v1 (Stripe шлёт две при смене секрета), сравнение за постоянное время.
func validSignature(body []byte, header, secret string, now time.Time) bool {
	if secret == "" {
		return false
	}
	var ts string
	var sigs []string
	for _, part := range strings.Split(header, ",") {
		k, v, _ := strings.Cut(strings.TrimSpace(part), "=")
		switch k {
		case "t":
			ts = v
		case "v1":
			sigs = append(sigs, v)
		}
	}
	sec, err := strconv.ParseInt(ts, 10, 64)
	if err != nil || len(sigs) == 0 {
		return false
	}
	if d := now.Sub(time.Unix(sec, 0)); d > tolerance || d < -tolerance {
		return false
	}
	want := sign(ts, body, secret)
	ok := false
	for _, s := range sigs {
		got, err := hex.DecodeString(s)
		if err == nil && hmac.Equal(got, want) {
			ok = true
		}
	}
	return ok
}

func sign(ts string, body []byte, secret string) []byte {
	m := hmac.New(sha256.New, []byte(secret))
	m.Write([]byte(ts + "."))
	m.Write(body)
	return m.Sum(nil)
}
