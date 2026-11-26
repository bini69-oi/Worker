package tabpay

import (
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/bini69-oi/worker/core"
	"github.com/bini69-oi/worker/pay/gatewaytest"
)

type cfg map[string]string

func (c cfg) String(k string) string { return c[k] }
func (c cfg) Secret(k string) string { return c[k] }

var full = cfg{keyAPI: "tp_test", keySecret: "whsec_test", keyMethod: "sbp"}

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

// fakeAPI проверяет ключ и отдаёт записанные ответы.
func fakeAPI(t *testing.T, statusFile string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Api-Key") != "tp_test" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(read(t, "create_error.json")))
			return
		}
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/payments":
			var req map[string]any
			_ = json.NewDecoder(r.Body).Decode(&req)
			if req["amountKopecks"] != float64(30000) || req["method"] != "SBP" ||
				!strings.HasPrefix(req["orderId"].(string), orderPrefix) {
				t.Errorf("платёж создан не так: %v", req)
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(read(t, "create_ok.json")))
		case r.Method == http.MethodGet && r.URL.Path == "/v1/payments/"+extID:
			_, _ = w.Write([]byte(strings.ReplaceAll(read(t, statusFile), "{{AMOUNT}}", "30000")))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func stamp() string { return strconv.FormatInt(time.Now().Unix(), 10) }

func signed(body, key, ts string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/pay/tabpay", strings.NewReader(body))
	r.Header.Set("X-Timestamp", ts)
	r.Header.Set("X-Signature-V2", hex.EncodeToString(sign(ts, []byte(body), key)))
	return r
}

func hook(p core.Payment, amount int64, key string) *http.Request {
	body := strings.NewReplacer("{{ORDER}}", orderPrefix+strconv.FormatInt(p.ID, 10),
		"{{AMOUNT}}", strconv.FormatInt(amount, 10)).Replace(webhookTmpl)
	return signed(body, key, stamp())
}

func TestSuite(t *testing.T) {
	srv := fakeAPI(t, "status_paid.json")
	gatewaytest.Run(t, gatewaytest.Case{
		Gateway:      New(full, srv.URL, srv.Client()),
		Unconfigured: New(cfg{}, srv.URL, srv.Client()),
		Paid:         func(p core.Payment, a int64) *http.Request { return hook(p, a, "whsec_test") },
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
	if _, err := g.Verify(hook(core.Payment{ID: 1}, 30000, "whsec_test")); err == nil {
		t.Fatal("оплата принята без подтверждения API")
	}
}

// API недоступен: вебхук не принимается, касса повторит его позже.
func TestAPIDown(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	g := New(full, srv.URL, srv.Client())
	srv.Close()
	if _, err := g.Verify(hook(core.Payment{ID: 1}, 30000, "whsec_test")); err == nil {
		t.Fatal("оплата принята при недоступном API")
	}
}

func TestBadSignatures(t *testing.T) {
	srv := fakeAPI(t, "status_paid.json")
	g := New(full, srv.URL, srv.Client())
	body := `{"id":"` + extID + `","status":"SUCCESS","amountKopecks":30000}`
	old := strconv.FormatInt(time.Now().Add(-time.Hour).Unix(), 10)
	noSig := httptest.NewRequest(http.MethodPost, "/pay/tabpay", strings.NewReader(body))
	badTS := signed(body, "whsec_test", stamp())
	badTS.Header.Set("X-Timestamp", "abc")
	cases := map[string]*http.Request{
		"без подписи":   noSig,
		"чужой ключ":    signed(body, "x", stamp()),
		"старая метка":  signed(body, "whsec_test", old),
		"метка не цифр": badTS,
	}
	for name, r := range cases {
		if _, err := g.Verify(r); err == nil {
			t.Errorf("%s: вебхук принят", name)
		}
	}
	if _, err := New(cfg{keyAPI: "k"}, srv.URL, srv.Client()).Verify(signed(body, "", stamp())); err == nil {
		t.Error("пустой секрет принят")
	}
}

func TestTestPaymentRejected(t *testing.T) {
	srv := fakeAPI(t, "status_paid.json")
	g := New(full, srv.URL, srv.Client())
	body := `{"id":"` + extID + `","orderId":"worker-1","status":"SUCCESS","amountKopecks":30000,"test":true}`
	if _, err := g.Verify(signed(body, "whsec_test", stamp())); err == nil {
		t.Fatal("тестовый платёж принят")
	}
}

func TestSecretNotInError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "tp_test", 500) }))
	defer srv.Close()
	_, err := New(full, srv.URL, srv.Client()).Status(t.Context(), core.Payment{ExternalID: "x"})
	if err == nil || strings.Contains(err.Error(), "tp_test") {
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
