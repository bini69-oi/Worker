package rollypay

import (
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/bini69-oi/worker/core"
	"github.com/bini69-oi/worker/pay/gatewaytest"
)

type cfg map[string]string

func (c cfg) String(k string) string { return c[k] }
func (c cfg) Secret(k string) string { return c[k] }

var full = cfg{keyAPI: "api-test", keySecret: "sig-test"}

func read(t testing.TB, name string) string {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// fakeAPI проверяет ключ и отдаёт записанные ответы.
func fakeAPI(t *testing.T, statusFile string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-API-Key") != "api-test" || r.Header.Get("X-Nonce") == "" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(read(t, "create_error.json")))
			return
		}
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/payments":
			var req map[string]any
			_ = json.NewDecoder(r.Body).Decode(&req)
			if req["amount"] != "300.00" || req["payment_currency"] != "RUB" || !strings.HasPrefix(req["order_id"].(string), orderPrefix) {
				t.Errorf("платёж создан не так: %v", req)
			}
			_, _ = w.Write([]byte(read(t, "create_ok.json")))
		case r.Method == http.MethodGet && r.URL.Path == "/payments/rp-7f3a":
			_, _ = w.Write([]byte(strings.ReplaceAll(read(t, statusFile), "{{AMOUNT}}", "300.00")))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func signed(body, secret string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/pay/rollypay", strings.NewReader(body))
	r.Header.Set("X-Timestamp", "1795000000")
	r.Header.Set("X-Signature", hex.EncodeToString(sign("1795000000", []byte(body), secret)))
	return r
}

func hook(p core.Payment, amount int64, secret string) *http.Request {
	body := strings.NewReplacer("{{ORDER}}", orderPrefix+strconv.FormatInt(p.ID, 10),
		"{{AMOUNT}}", rubles(amount)).Replace(mustRead("webhook.tmpl"))
	return signed(body, secret)
}

func TestSuite(t *testing.T) {
	srv := fakeAPI(t, "status_paid.json")
	gatewaytest.Run(t, gatewaytest.Case{
		Gateway:      New(full, srv.URL, srv.Client()),
		Unconfigured: New(cfg{}, srv.URL, srv.Client()),
		Paid:         func(p core.Payment, a int64) *http.Request { return hook(p, a, "sig-test") },
		Forged:       func(p core.Payment, a int64) *http.Request { return hook(p, a, "чужой") },
		Amount:       30000,
	})
}

func TestStatus(t *testing.T) {
	for file, want := range map[string]core.PayStatus{
		"status_paid.json": core.PayPaid, "status_pending.json": core.PayPending, "status_chargeback.json": core.PayRefunded,
	} {
		srv := fakeAPI(t, file)
		ev, err := New(full, srv.URL, srv.Client()).Status(t.Context(), core.Payment{ExternalID: "rp-7f3a"})
		if err != nil || ev.Status != want {
			t.Fatalf("%s: %+v %v", file, ev, err)
		}
		if want == core.PayPaid && ev.ExtAmount != 30000 {
			t.Fatalf("сумма %d", ev.ExtAmount)
		}
	}
}

// Вебхук с верной подписью, но API говорит «не оплачено»: верим API.
func TestHookNotConfirmed(t *testing.T) {
	srv := fakeAPI(t, "status_pending.json")
	g := New(full, srv.URL, srv.Client())
	if _, err := g.Verify(hook(core.Payment{ID: 1}, 30000, "sig-test")); err == nil {
		t.Fatal("оплата принята без подтверждения API")
	}
}

func TestBadSignatures(t *testing.T) {
	srv := fakeAPI(t, "status_paid.json")
	g := New(full, srv.URL, srv.Client())
	body := `{"payment_id":"rp-7f3a","status":"paid","amount":"300.00"}`
	noSig := httptest.NewRequest(http.MethodPost, "/pay/rollypay", strings.NewReader(body))
	badTime := signed(body, "sig-test")
	badTime.Header.Set("X-Timestamp", "1795000001")
	for name, r := range map[string]*http.Request{"без подписи": noSig, "другое время": badTime} {
		if _, err := g.Verify(r); err == nil {
			t.Errorf("%s: вебхук принят", name)
		}
	}
	if _, err := New(cfg{keyAPI: "api-test"}, srv.URL, srv.Client()).Verify(signed(body, "")); err == nil {
		t.Error("пустой секрет принят")
	}
}

func TestSecretNotInError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "api-test", 500) }))
	defer srv.Close()
	_, err := New(full, srv.URL, srv.Client()).Status(t.Context(), core.Payment{ExternalID: "x"})
	if err == nil || strings.Contains(err.Error(), "api-test") {
		t.Fatalf("ошибка: %v", err)
	}
}

func TestMoney(t *testing.T) {
	for in, want := range map[string]int64{"300": 30000, "300.5": 30050, "0.01": 1, "12.3400": 1234} {
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

func mustRead(name string) string {
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		panic(err)
	}
	return string(b)
}
