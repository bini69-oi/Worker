package platega

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/bini69-oi/worker/core"
	"github.com/bini69-oi/worker/pay/gatewaytest"
)

type cfg map[string]string

func (c cfg) String(k string) string { return c[k] }
func (c cfg) Secret(k string) string { return c[k] }
func (c cfg) Int(k string) int64 {
	var n int64
	for _, r := range c[k] {
		n = n*10 + int64(r-'0')
	}
	return n
}

// fakeAPI изображает API Platega: помнит созданные транзакции и проверяет заголовки.
type fakeAPI struct {
	mu  sync.Mutex
	txs map[string]map[string]any
}

func (f *fakeAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("X-MerchantId") != "shop" || r.Header.Get("X-Secret") != "s3cret" {
		http.Error(w, "no", http.StatusUnauthorized)
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	switch {
	case r.Method == http.MethodPost && r.URL.Path == "/transaction/process":
		var in struct {
			PaymentDetails struct {
				Amount json.Number `json:"amount"`
			} `json:"paymentDetails"`
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		id := "tx-" + string(rune('a'+len(f.txs)))
		f.txs[id] = map[string]any{"id": id, "status": "CONFIRMED", "paymentDetails": map[string]any{"amount": in.PaymentDetails.Amount, "currency": "RUB"}}
		_ = json.NewEncoder(w).Encode(map[string]any{"transactionId": id, "status": "PENDING", "redirect": "https://pay.test/" + id})
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/transaction/"):
		tx, ok := f.txs[strings.TrimPrefix(r.URL.Path, "/transaction/")]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(tx)
	default:
		http.NotFound(w, r)
	}
}

func hook(p core.Payment, amount int64, secret string) *http.Request {
	body, _ := json.Marshal(map[string]any{"id": p.ExternalID, "amount": json.Number(rubles(amount)),
		"currency": "RUB", "status": "CONFIRMED", "payload": "worker:" + string(rune('0'+p.ID))})
	r := httptest.NewRequest(http.MethodPost, "/pay/platega", strings.NewReader(string(body)))
	r.Header.Set("X-MerchantId", "shop")
	r.Header.Set("X-Secret", secret)
	return r
}

func TestGateway(t *testing.T) {
	api := &fakeAPI{txs: map[string]map[string]any{}}
	srv := httptest.NewServer(api)
	defer srv.Close()
	full := cfg{"platega.merchant_id": "shop", "platega.secret": "s3cret", "platega.method": "2"}
	g := New(full, srv.URL, srv.Client())
	gatewaytest.Run(t, gatewaytest.Case{
		Gateway:      g,
		Unconfigured: New(cfg{}, srv.URL, srv.Client()),
		Paid:         func(p core.Payment, a int64) *http.Request { return hook(p, a, "s3cret") },
		Forged:       func(p core.Payment, a int64) *http.Request { return hook(p, a, "чужой") },
		Amount:       30000,
	})
}

func TestStatus(t *testing.T) {
	api := &fakeAPI{txs: map[string]map[string]any{"x": {"id": "x", "status": "CANCELED"}, "y": {"id": "y", "status": "CONFIRMED", "paymentDetails": map[string]any{"amount": 150.5}}}}
	srv := httptest.NewServer(api)
	defer srv.Close()
	g := New(cfg{"platega.merchant_id": "shop", "platega.secret": "s3cret"}, srv.URL, srv.Client())
	ev, err := g.Status(t.Context(), core.Payment{ExternalID: "x", ExtAmount: 100})
	if err != nil || ev.Status != core.PayFailed {
		t.Fatalf("отмена: %+v %v", ev, err)
	}
	ev, err = g.Status(t.Context(), core.Payment{ExternalID: "y"})
	if err != nil || ev.Status != core.PayPaid || ev.ExtAmount != 15050 {
		t.Fatalf("оплата: %+v %v", ev, err)
	}
}

func TestSecretNotInError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "s3cret", 500) }))
	defer srv.Close()
	g := New(cfg{"platega.merchant_id": "shop", "platega.secret": "s3cret"}, srv.URL, srv.Client())
	_, err := g.Status(t.Context(), core.Payment{ExternalID: "x"})
	if err == nil || strings.Contains(err.Error(), "s3cret") {
		t.Fatalf("ошибка: %v", err)
	}
}

func TestKopecks(t *testing.T) {
	for in, want := range map[string]int64{"300": 30000, "300.5": 30050, "300.50": 30050, "0.01": 1, "12.3400": 1234} {
		if got, err := kopecks(in); err != nil || got != want {
			t.Errorf("%s: %d %v", in, got, err)
		}
	}
	if _, err := kopecks("1.005"); err == nil {
		t.Error("доли копейки приняты")
	}
	if rubles(12305) != "123.05" {
		t.Error("rubles")
	}
}

func TestChargebackAndCurrency(t *testing.T) {
	api := &fakeAPI{txs: map[string]map[string]any{
		"cb":  {"id": "cb", "status": "CHARGEBACKED"},
		"usd": {"id": "usd", "status": "CONFIRMED", "paymentDetails": map[string]any{"amount": 300, "currency": "USD"}},
		"nil": {"id": "nil", "status": "CONFIRMED"},
	}}
	srv := httptest.NewServer(api)
	defer srv.Close()
	g := New(cfg{"platega.merchant_id": "shop", "platega.secret": "s3cret"}, srv.URL, srv.Client())
	body, _ := json.Marshal(map[string]any{"id": "cb", "status": "CHARGEBACKED"})
	r := httptest.NewRequest(http.MethodPost, "/pay/platega", strings.NewReader(string(body)))
	r.Header.Set("X-MerchantId", "shop")
	r.Header.Set("X-Secret", "s3cret")
	if ev, err := g.Verify(r); err != nil || ev.Status != core.PayRefunded {
		t.Fatalf("оспаривание должно стать возвратом: %+v %v", ev, err)
	}
	if _, err := g.Status(t.Context(), core.Payment{ExternalID: "usd", ExtAmount: 30000}); err == nil {
		t.Fatal("оплата в долларах принята как рубли")
	}
	if _, err := g.Status(t.Context(), core.Payment{ExternalID: "nil", ExtAmount: 30000}); err == nil {
		t.Fatal("оплата без суммы принята с нашей суммой")
	}
}
