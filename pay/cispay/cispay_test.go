package cispay

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

var full = cfg{keyShop: "shop-test", keyAPI: "cis_sec_test", keyMethod: "sbp"}

const extID = "0192f6d1-7a3c-7c1e-9b11-5e2a4d6f8a01"

var webhookTmpl = func() string {
	b, err := os.ReadFile("testdata/webhook.tmpl")
	if err != nil {
		panic(err)
	}
	return string(b)
}()

func read(t testing.TB, name string) string {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// fakeAPI проверяет ключи и отдаёт записанные ответы.
func fakeAPI(t *testing.T, statusFile string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Shop-ID") != "shop-test" || r.Header.Get("X-Api-Key") != "cis_sec_test" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(read(t, "create_error.json")))
			return
		}
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/payments":
			var req map[string]any
			_ = json.NewDecoder(r.Body).Decode(&req)
			if req["amount"] != float64(30000) || req["payment_method"] != "SBP" || req["customer_id"] == "" ||
				!strings.HasPrefix(req["order_id"].(string), orderPrefix) {
				t.Errorf("платёж создан не так: %v", req)
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(read(t, "create_ok.json")))
		case r.Method == http.MethodGet && r.URL.Path == "/payments/status" && r.URL.Query().Get("id") == extID:
			_, _ = w.Write([]byte(strings.ReplaceAll(read(t, statusFile), "{{AMOUNT}}", "30000")))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func signed(body, key string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/pay/cispay", strings.NewReader(body))
	r.Header.Set("X-Signature", hex.EncodeToString(sign([]byte(body), key)))
	return r
}

func hook(p core.Payment, amount int64, key string) *http.Request {
	body := strings.NewReplacer("{{ORDER}}", orderPrefix+strconv.FormatInt(p.ID, 10),
		"{{AMOUNT}}", strconv.FormatInt(amount, 10)).Replace(webhookTmpl)
	return signed(body, key)
}

func TestSuite(t *testing.T) {
	srv := fakeAPI(t, "status_paid.json")
	gatewaytest.Run(t, gatewaytest.Case{
		Gateway:      New(full, srv.URL, srv.Client()),
		Unconfigured: New(cfg{}, srv.URL, srv.Client()),
		Paid:         func(p core.Payment, a int64) *http.Request { return hook(p, a, "cis_sec_test") },
		Forged:       func(p core.Payment, a int64) *http.Request { return hook(p, a, "чужой") },
		Amount:       30000,
	})
}

func TestStatus(t *testing.T) {
	for file, want := range map[string]core.PayStatus{
		"status_paid.json": core.PayPaid, "status_pending.json": core.PayPending, "status_refunded.json": core.PayRefunded,
	} {
		srv := fakeAPI(t, file)
		ev, err := New(full, srv.URL, srv.Client()).Status(t.Context(), core.Payment{ExternalID: extID})
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
	if _, err := g.Verify(hook(core.Payment{ID: 1}, 30000, "cis_sec_test")); err == nil {
		t.Fatal("оплата принята без подтверждения API")
	}
}

func TestBadSignatures(t *testing.T) {
	srv := fakeAPI(t, "status_paid.json")
	g := New(full, srv.URL, srv.Client())
	body := `{"id":"` + extID + `","status":"PAID","amount":30000}`
	noSig := httptest.NewRequest(http.MethodPost, "/pay/cispay", strings.NewReader(body))
	for name, r := range map[string]*http.Request{"без подписи": noSig, "чужой ключ": signed(body, "x")} {
		if _, err := g.Verify(r); err == nil {
			t.Errorf("%s: вебхук принят", name)
		}
	}
	if _, err := New(cfg{keyShop: "s"}, srv.URL, srv.Client()).Verify(signed(body, "")); err == nil {
		t.Error("пустой ключ принят")
	}
}

func TestSecretNotInError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "cis_sec_test", 500) }))
	defer srv.Close()
	_, err := New(full, srv.URL, srv.Client()).Status(t.Context(), core.Payment{ExternalID: "x"})
	if err == nil || strings.Contains(err.Error(), "cis_sec_test") {
		t.Fatalf("ошибка: %v", err)
	}
}

func TestAmountNotInteger(t *testing.T) {
	for _, in := range []string{"", "300.5", "abc", "-1"} {
		if _, err := (apiPay{Amount: json.Number(in)}).kopecks(); err == nil {
			t.Errorf("%q принято", in)
		}
	}
}
