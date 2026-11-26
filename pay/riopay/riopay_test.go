package riopay

import (
	"encoding/hex"
	"encoding/json"
	"github.com/bini69-oi/worker/pay"
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

var full = cfg{keyToken: "tok-test", keySecret: "sig-test"}

const orderID = "7b1c2d3e-0001-4a5b-9c8d-aaaaaaaaaaaa"

func read(t testing.TB, name string) string {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// fakeAPI проверяет токен и отдаёт записанные ответы.
func fakeAPI(t *testing.T, statusFile string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Api-Token") != "tok-test" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(read(t, "create_error.json")))
			return
		}
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/orders":
			var req map[string]any
			_ = json.NewDecoder(r.Body).Decode(&req)
			if req["amount"] != "300.00" || req["externalId"] == "" || req["purpose"] == "" {
				t.Errorf("заказ создан не так: %v", req)
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(read(t, "create_ok.json")))
		case r.Method == http.MethodGet && r.URL.Path == "/orders/"+orderID:
			_, _ = w.Write([]byte(strings.ReplaceAll(read(t, statusFile), "{{AMOUNT}}", "300.00")))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func signed(body, secret string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/pay/riopay", strings.NewReader(body))
	r.Header.Set("X-Signature", hex.EncodeToString(sign([]byte(body), secret)))
	return r
}

func hook(p core.Payment, amount int64, secret string) *http.Request {
	body := strings.NewReplacer("{{ORDER}}", strconv.FormatInt(p.ID, 10), "{{AMOUNT}}", pay.Rubles(amount)).
		Replace(mustRead("webhook.tmpl"))
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
		"status_paid.json": core.PayPaid, "status_pending.json": core.PayPending, "status_expired.json": core.PayFailed,
	} {
		srv := fakeAPI(t, file)
		ev, err := New(full, srv.URL, srv.Client()).Status(t.Context(), core.Payment{ExternalID: orderID})
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

// Ключ подписи не задан: подписывает API-токен.
func TestSecretFallsBackToToken(t *testing.T) {
	srv := fakeAPI(t, "status_paid.json")
	g := New(cfg{keyToken: "tok-test"}, srv.URL, srv.Client())
	ev, err := g.Verify(hook(core.Payment{ID: 1}, 30000, "tok-test"))
	if err != nil || ev.Status != core.PayPaid || ev.PaymentID != 1 || ev.ExtAmount != 30000 {
		t.Fatalf("%+v %v", ev, err)
	}
}

func TestBadSignatures(t *testing.T) {
	srv := fakeAPI(t, "status_paid.json")
	g := New(full, srv.URL, srv.Client())
	body := `{"id":"` + orderID + `","status":"COMPLETED","amount":300}`
	noSig := httptest.NewRequest(http.MethodPost, "/pay/riopay", strings.NewReader(body))
	for name, r := range map[string]*http.Request{"без подписи": noSig, "не hex": signedRaw(body, "zz")} {
		if _, err := g.Verify(r); err == nil {
			t.Errorf("%s: вебхук принят", name)
		}
	}
	if _, err := New(cfg{}, srv.URL, srv.Client()).Verify(signed(body, "")); err == nil {
		t.Error("пустой секрет принят")
	}
}

func signedRaw(body, sig string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/pay/riopay", strings.NewReader(body))
	r.Header.Set("X-Signature", sig)
	return r
}

func TestSecretNotInError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "tok-test", 500) }))
	defer srv.Close()
	_, err := New(full, srv.URL, srv.Client()).Status(t.Context(), core.Payment{ExternalID: "x"})
	if err == nil || strings.Contains(err.Error(), "tok-test") {
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
}

func mustRead(name string) string {
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		panic(err)
	}
	return string(b)
}
