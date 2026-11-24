package gatewaytest

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"

	"github.com/bini69-oi/worker/core"
	"github.com/bini69-oi/worker/pay"
	"github.com/bini69-oi/worker/settings"
)

func init() {
	settings.AddGateway("fake", "Тестовая касса")
}

// Fake это простейшая касса: вебхук в JSON, подпись HMAC-SHA256 от тела в заголовке X-Signature.
// Она нужна, чтобы проверить сам набор тестов, и служит образцом для новой кассы.
type Fake struct {
	Secret string
	// Paid это номера платежей, которые касса считает оплаченными при сверке.
	Paid map[string]bool
}

type fakeHook struct {
	PaymentID int64  `json:"payment_id"`
	ID        string `json:"id"`
	Amount    int64  `json:"amount"`
	Status    string `json:"status"`
}

func (f *Fake) Name() string     { return "fake" }
func (f *Fake) Configured() bool { return f.Secret != "" }

func (f *Fake) Create(_ context.Context, p core.Payment) (core.Invoice, error) {
	return core.Invoice{ExternalID: fmt.Sprintf("fake-%d", p.ID), URL: "https://pay.test/fake", ExtAmount: p.Amount, ExtCurrency: "RUB"}, nil
}

func (f *Fake) Status(_ context.Context, p core.Payment) (core.GatewayEvent, error) {
	ev := core.GatewayEvent{ExternalID: p.ExternalID, ExtAmount: p.ExtAmount, Status: core.PayPending}
	if f.Paid[p.ExternalID] {
		ev.Status = core.PayPaid
	}
	return ev, nil
}

func (f *Fake) Verify(r *http.Request) (core.GatewayEvent, error) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		return core.GatewayEvent{}, err
	}
	if f.Secret == "" || !hmac.Equal([]byte(r.Header.Get("X-Signature")), []byte(f.sign(body))) {
		return core.GatewayEvent{}, pay.ErrBadSignature
	}
	var h fakeHook
	if err := json.Unmarshal(body, &h); err != nil {
		return core.GatewayEvent{}, err
	}
	ev := core.GatewayEvent{PaymentID: h.PaymentID, ExternalID: h.ID, ExtAmount: h.Amount, Status: core.PayPending}
	if h.Status == "paid" {
		ev.Status = core.PayPaid
	}
	return ev, nil
}

func (f *Fake) sign(body []byte) string {
	m := hmac.New(sha256.New, []byte(f.Secret))
	m.Write(body)
	return hex.EncodeToString(m.Sum(nil))
}

// Hook собирает вебхук фейковой кассы. Пустой secret означает подпись чужим ключом.
func (f *Fake) Hook(p core.Payment, amount int64, forged bool) *http.Request {
	body, _ := json.Marshal(fakeHook{PaymentID: p.ID, ID: p.ExternalID, Amount: amount, Status: "paid"})
	r := httptest.NewRequest(http.MethodPost, "/pay/fake", strings.NewReader(string(body)))
	sig := f.sign(body)
	if forged {
		sig = (&Fake{Secret: "чужой"}).sign(body)
	}
	r.Header.Set("X-Signature", sig)
	return r
}
