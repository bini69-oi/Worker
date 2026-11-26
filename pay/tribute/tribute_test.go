package tribute

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/bini69-oi/worker/core"
	"github.com/bini69-oi/worker/pay/gatewaytest"
)

type cfg map[string]string

func (c cfg) String(k string) string { return c[k] }
func (c cfg) Secret(k string) string { return c[k] }

const (
	apiKey = "tr-key"
	tgID   = 1001 // Telegram ID, под которым gatewaytest регистрирует абонента
)

// book изображает выборку сборки: платежи, созданные кассой, и ключи уже привязанных донатов.
type book struct {
	mu    sync.Mutex
	list  []core.Payment
	byKey map[string]int64
}

type recorder struct {
	*Gateway
	b *book
}

func (r recorder) Create(ctx context.Context, p core.Payment) (core.Invoice, error) {
	r.b.mu.Lock()
	r.b.list = append(r.b.list, p)
	r.b.mu.Unlock()
	return r.Gateway.Create(ctx, p)
}

func (b *book) pending(_ context.Context, tg, amount int64, key string) (core.Payment, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if id, ok := b.byKey[key]; ok {
		return core.Payment{ID: id}, nil
	}
	if tg != tgID {
		return core.Payment{}, core.ErrNotFound
	}
	for i := len(b.list) - 1; i >= 0; i-- { // последний созданный: в наборе у каждого случая свой платёж
		if p := b.list[i]; p.Amount == amount {
			b.byKey[key] = p.ID
			return p, nil
		}
	}
	return core.Payment{}, core.ErrNotFound
}

func sign(key string, body []byte) string {
	m := hmac.New(sha256.New, []byte(key))
	m.Write(body)
	return hex.EncodeToString(m.Sum(nil))
}

func donation(t *testing.T, file, key string, amount int64, created string) *http.Request {
	t.Helper()
	raw, err := os.ReadFile("testdata/" + file)
	if err != nil {
		t.Fatal(err)
	}
	body := string(raw)
	if amount != 0 {
		body = strings.Replace(body, `"amount":30000`, `"amount":`+strconv.FormatInt(amount, 10), 1)
	}
	if created != "" {
		body = strings.Replace(body, "2026-11-20T10:15:30.123Z", created, 1)
	}
	r := httptest.NewRequest(http.MethodPost, "/pay/tribute", strings.NewReader(body))
	r.Header.Set("trbt-signature", sign(key, []byte(body)))
	return r
}

func newGateway(b *book) *Gateway {
	g := New(cfg{keyAPI: apiKey, keyLink: "https://t.me/tribute/app?startapp=dAbC"})
	g.Pending = b.pending
	return g
}

func TestSuite(t *testing.T) {
	b := &book{byKey: map[string]int64{}}
	g := newGateway(b)
	gatewaytest.Run(t, gatewaytest.Case{
		Gateway:      recorder{g, b},
		Unconfigured: New(cfg{}),
		Amount:       30000,
		Paid: func(_ core.Payment, amount int64) *http.Request {
			return donation(t, "new_donation.json", apiKey, amount, "")
		},
		Forged: func(_ core.Payment, amount int64) *http.Request {
			return donation(t, "new_donation.json", "чужой", amount, "")
		},
	})
}

func TestVerify(t *testing.T) {
	b := &book{byKey: map[string]int64{}}
	g := newGateway(b)
	b.list = []core.Payment{{ID: 7, Amount: 30000}, {ID: 8, Amount: 15050}}
	ev, err := g.Verify(donation(t, "new_donation.json", apiKey, 0, ""))
	if err != nil || ev.Status != core.PayPaid || ev.PaymentID != 7 || ev.ExtAmount != 30000 ||
		ev.ExternalID != "4821_1001_30000_2026-11-20T10:15:30.123Z" {
		t.Fatalf("новый донат: %+v %v", ev, err)
	}
	ev, err = g.Verify(donation(t, "recurrent_donation.json", apiKey, 0, ""))
	if err != nil || ev.PaymentID != 8 || ev.ExtAmount != 15050 {
		t.Fatalf("повторный донат: %+v %v", ev, err)
	}
	ev, err = g.Verify(donation(t, "cancelled_subscription.json", apiKey, 0, ""))
	if err != nil || ev.Status != core.PayPending || ev.PaymentID != 0 {
		t.Fatalf("отмена подписки не должна зачислять: %+v %v", ev, err)
	}
	ev, err = g.Verify(donation(t, "new_donation.json", apiKey, 99900, ""))
	if err != nil || ev.PaymentID != 0 || ev.Status != core.PayPaid {
		t.Fatalf("донат без платежа: %+v %v", ev, err)
	}
	if _, err = g.Verify(donation(t, "new_donation.json", "чужой", 0, "")); err == nil {
		t.Fatal("чужая подпись принята")
	}
	r := donation(t, "new_donation.json", apiKey, 0, "")
	r.Header.Del("trbt-signature")
	if _, err = g.Verify(r); err == nil {
		t.Fatal("без подписи принят")
	}
}

func TestCreateAndStatus(t *testing.T) {
	g := newGateway(&book{})
	inv, err := g.Create(t.Context(), core.Payment{ID: 1, Amount: 30000})
	if err != nil || inv.URL == "" || inv.ExtAmount != 30000 || inv.ExtCurrency != "RUB" {
		t.Fatalf("счёт: %+v %v", inv, err)
	}
	ev, err := g.Status(t.Context(), core.Payment{ID: 1})
	if err != nil || ev.Status != core.PayPending {
		t.Fatalf("статус: %+v %v", ev, err)
	}
	if New(cfg{keyAPI: "k", keyLink: "l"}).Configured() {
		t.Fatal("касса без Pending считается настроенной")
	}
}
