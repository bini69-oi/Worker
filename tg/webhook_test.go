package tg

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bini69-oi/worker/tgsign"
)

func post(h http.Handler, secret, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, "/hook", strings.NewReader(body))
	if secret != "" {
		r.Header.Set(tgsign.WebhookSecretHeader, secret)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestWebhook(t *testing.T) {
	cases := []struct {
		name, secret, body string
		want               int
		queued             bool
	}{
		{"без секрета", "", `{"update_id":1}`, 401, false},
		{"чужой секрет", "bad", `{"update_id":1}`, 401, false},
		{"ок", "s3cret", `{"update_id":1,"message":{"message_id":2,"chat":{"id":3},"text":"x"}}`, 200, true},
		{"битый JSON", "s3cret", `{`, 400, false},
		{"слишком большое", "s3cret", `{"update_id":1,"x":"` + strings.Repeat("a", maxWebhookBody) + `"}`, 413, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ch := make(chan Update, 1)
			w := post(NewWebhook("s3cret", ch, nil), tc.secret, tc.body)
			if w.Code != tc.want {
				t.Fatalf("код %d, ждали %d", w.Code, tc.want)
			}
			if tc.queued != (len(ch) == 1) {
				t.Fatalf("в канале %d", len(ch))
			}
		})
	}
}

func TestWebhookWrongMethod(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/hook", nil)
	r.Header.Set(tgsign.WebhookSecretHeader, "s")
	w := httptest.NewRecorder()
	NewWebhook("s", make(chan Update, 1), nil).ServeHTTP(w, r)
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("код %d", w.Code)
	}
}

// Ответ уходит до обработки: канал никто не читает, пока Telegram не получил 200.
func TestWebhookAnswersBeforeProcessing(t *testing.T) {
	ch := make(chan Update, 4)
	w := post(NewWebhook("s", ch, nil), "s", `{"update_id":7}`)
	if w.Code != 200 {
		t.Fatalf("код %d", w.Code)
	}
	if u := <-ch; u.UpdateID != 7 {
		t.Fatalf("update_id %d", u.UpdateID)
	}
}

func TestWebhookFullChannel503(t *testing.T) {
	ch := make(chan Update, 1)
	h := NewWebhook("s", ch, nil)
	if w := post(h, "s", `{"update_id":1}`); w.Code != 200 {
		t.Fatalf("код %d", w.Code)
	}
	if w := post(h, "s", `{"update_id":2}`); w.Code != http.StatusServiceUnavailable {
		t.Fatalf("код %d, ждали 503", w.Code)
	}
}

func TestWebhookUrgent(t *testing.T) {
	ch := make(chan Update, 1)
	fail := true
	urgent := func(_ context.Context, u Update) (bool, error) {
		if u.Message == nil || u.Message.SuccessfulPayment == nil {
			return false, nil
		}
		if fail {
			return true, errors.New("база занята")
		}
		return true, nil
	}
	h := NewWebhook("s", ch, urgent)
	pay := `{"update_id":9,"message":{"message_id":1,"chat":{"id":1},"successful_payment":{"currency":"XTR","total_amount":1,"invoice_payload":"p:1","telegram_payment_charge_id":"c"}}}`
	if w := post(h, "s", pay); w.Code != http.StatusInternalServerError {
		t.Fatalf("сбой оплаты: код %d, ждали 500, чтобы Telegram повторил", w.Code)
	}
	fail = false
	if w := post(h, "s", pay); w.Code != http.StatusOK || len(ch) != 0 {
		t.Fatalf("оплата: код %d, в очереди %d", w.Code, len(ch))
	}
	if w := post(h, "s", `{"update_id":10}`); w.Code != http.StatusOK || len(ch) != 1 {
		t.Fatalf("обычное обновление не в очереди: %d %d", w.Code, len(ch))
	}
}
