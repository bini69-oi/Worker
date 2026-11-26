package paypear

import (
	"encoding/json"
	"github.com/bini69-oi/worker/pay"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/bini69-oi/worker/core"
	"github.com/bini69-oi/worker/pay/gatewaytest"
)

type cfg map[string]string

func (c cfg) String(k string) string { return c[k] }
func (c cfg) Secret(k string) string { return c[k] }

var full = cfg{keyShop: "shop-test", keySecret: "key-test"}

func read(t testing.TB, name string) string {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// fakeAPI проверяет Basic-авторизацию и отдаёт записанные ответы. Неизвестный платёж это 404.
func fakeAPI(t *testing.T, statusFile string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if u, p, ok := r.BasicAuth(); !ok || u != "shop-test" || p != "key-test" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(read(t, "create_error.json")))
			return
		}
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/payment/":
			var req map[string]any
			_ = json.NewDecoder(r.Body).Decode(&req)
			amount, _ := req["amount"].(map[string]any)
			order, _ := req["order_id"].(string)
			if amount["value"] != "300.00" || amount["currency"] != "RUB" || !strings.HasPrefix(order, orderPrefix) ||
				r.Header.Get("Idempotence-Key") == "" {
				t.Errorf("платёж создан не так: %v", req)
			}
			_, _ = w.Write([]byte(read(t, "create_ok.json")))
		case r.Method == http.MethodGet && r.URL.Path == "/payment/pp-4c1e/":
			_, _ = w.Write([]byte(read(t, statusFile)))
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"success":false,"message":"not found"}`))
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func hook(t *testing.T, id string, amount int64) *http.Request {
	body := strings.NewReplacer("{{ID}}", id, "{{ORDER}}", "worker_1", "{{AMOUNT}}", pay.Rubles(amount)).
		Replace(read(t, "webhook.tmpl"))
	return httptest.NewRequest(http.MethodPost, "/pay/paypear", strings.NewReader(body))
}

func TestSuite(t *testing.T) {
	srv := fakeAPI(t, "status_paid.json")
	gatewaytest.Run(t, gatewaytest.Case{
		Gateway:      New(full, srv.URL, srv.Client()),
		Unconfigured: New(cfg{}, srv.URL, srv.Client()),
		Paid:         func(_ core.Payment, a int64) *http.Request { return hook(t, "pp-4c1e", a) },
		Forged:       func(_ core.Payment, a int64) *http.Request { return hook(t, "pp-чужой", a) },
		Amount:       30000,
	})
}

func TestStatus(t *testing.T) {
	for file, want := range map[string]core.PayStatus{
		"status_paid.json": core.PayPaid, "status_pending.json": core.PayPending,
		"status_refunded.json": core.PayRefunded, "status_expired.json": core.PayFailed,
	} {
		srv := fakeAPI(t, file)
		ev, err := New(full, srv.URL, srv.Client()).Status(t.Context(), core.Payment{ExternalID: "pp-4c1e"})
		if err != nil || ev.Status != want {
			t.Fatalf("%s: %+v %v", file, ev, err)
		}
		if want == core.PayPaid && (ev.ExtAmount != 31500) {
			t.Fatalf("сумма %d", ev.ExtAmount)
		}
	}
}

// Вебхук говорит «оплачено», API говорит «ждёт»: верим API.
func TestHookNotConfirmed(t *testing.T) {
	srv := fakeAPI(t, "status_pending.json")
	if _, err := New(full, srv.URL, srv.Client()).Verify(hook(t, "pp-4c1e", 31500)); err == nil {
		t.Fatal("оплата принята без подтверждения API")
	}
}

func TestBadHooks(t *testing.T) {
	srv := fakeAPI(t, "status_paid.json")
	g := New(full, srv.URL, srv.Client())
	for name, body := range map[string]string{"не JSON": "x", "без номера": `{"object":{"status":"CONFIRMED"}}`} {
		if _, err := g.Verify(httptest.NewRequest(http.MethodPost, "/pay/paypear", strings.NewReader(body))); err == nil {
			t.Errorf("%s: вебхук принят", name)
		}
	}
	if _, err := New(cfg{}, srv.URL, srv.Client()).Verify(hook(t, "pp-4c1e", 31500)); err == nil {
		t.Error("вебхук принят без ключей")
	}
	ev, err := g.Verify(hook(t, "pp-4c1e", 31499))
	if err != nil || ev.ExtAmount == 31500 {
		t.Errorf("чужая сумма не помечена: %+v %v", ev, err)
	}
}

func TestSecretNotInError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "key-test", 500) }))
	defer srv.Close()
	_, err := New(full, srv.URL, srv.Client()).Status(t.Context(), core.Payment{ExternalID: "x"})
	if err == nil || strings.Contains(err.Error(), "key-test") || strings.Contains(err.Error(), "shop-test") {
		t.Fatalf("ошибка: %v", err)
	}
}

func TestMoney(t *testing.T) {
	for in, want := range map[string]int64{"300": 30000, "300.5": 30050, "0.01": 1, "12.3400": 1234} {
		if got, err := pay.Kopecks(in); err != nil || got != want {
			t.Errorf("%s: %d %v", in, got, err)
		}
	}
	if _, err := pay.Kopecks("1.005"); err == nil {
		t.Error("доли копейки приняты")
	}
	if pay.Rubles(12305) != "123.05" {
		t.Error("rubles")
	}
	var m money
	if err := json.Unmarshal([]byte(`300.25`), &m); err != nil {
		t.Fatal(err)
	}
	if k, err := m.kopecks(); err != nil || k != 30025 {
		t.Errorf("число без объекта: %d %v", k, err)
	}
}
