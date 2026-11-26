package cloudpayments

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/bini69-oi/worker/core"
	"github.com/bini69-oi/worker/pay/gatewaytest"
)

const (
	testID     = "pk_test_1234"
	testSecret = "api-s3cret"
)

type cfg map[string]string

func (c cfg) String(k string) string { return c[k] }
func (c cfg) Secret(k string) string { return c[k] }
func (c cfg) Int(string) int64       { return 0 }

func good() cfg { return cfg{keyPublicID: testID, keySecret: testSecret} }

// sample берёт записанный пример и подставляет номер платежа и сумму.
func sample(t *testing.T, name string, id, amount int64) string {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return strings.NewReplacer("{{ID}}", strconv.FormatInt(id, 10), "{{AMOUNT}}", rubles(amount)).Replace(string(b))
}

// fakeAPI изображает API CloudPayments: проверяет Basic-вход и отдаёт записанные ответы.
func fakeAPI(t *testing.T, paid map[int64]int64) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if u, p, ok := r.BasicAuth(); !ok || u != testID || p != testSecret {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = io.WriteString(w, sample(t, "error_401.json", 0, 0))
			return
		}
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)
		switch r.URL.Path {
		case "/orders/create":
			if req["Currency"] != "RUB" || req["InvoiceId"] == "" || req["Amount"] == nil {
				t.Errorf("счёт создан не так: %v", req)
			}
			_, _ = io.WriteString(w, sample(t, "create_ok.json", 0, 0))
		case "/payments/find":
			id, _ := strconv.ParseInt(req["InvoiceId"].(string), 10, 64)
			if a, ok := paid[id]; ok {
				_, _ = io.WriteString(w, sample(t, "find_paid.json", id, a))
				return
			}
			_, _ = io.WriteString(w, sample(t, "find_notfound.json", 0, 0))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func hook(t *testing.T, name, secret string, id, amount int64) *http.Request {
	t.Helper()
	body := sample(t, name, id, amount)
	r := httptest.NewRequest(http.MethodPost, "/pay/cloudpayments", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Content-HMAC", sign([]byte(body), secret))
	return r
}

func TestSuite(t *testing.T) {
	srv := fakeAPI(t, nil)
	gatewaytest.Run(t, gatewaytest.Case{
		Gateway:      New(good(), srv.URL, srv.Client()),
		Unconfigured: New(cfg{}, srv.URL, srv.Client()),
		Paid: func(p core.Payment, a int64) *http.Request {
			return hook(t, "notify_pay.form", testSecret, p.ID, a)
		},
		Forged: func(p core.Payment, a int64) *http.Request {
			return hook(t, "notify_pay.form", "чужой", p.ID, a)
		},
		Amount: 30000,
	})
}

func TestVerifyTable(t *testing.T) {
	g := New(good(), "", nil)
	tests := []struct {
		name, file string
		want       core.PayStatus
		amount     int64
	}{
		{"оплата", "notify_pay.form", core.PayPaid, 30050},
		{"возврат", "notify_refund.form", core.PayRefunded, 30050},
		{"отказ", "notify_fail.form", core.PayFailed, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ev, err := g.Verify(hook(t, tc.file, testSecret, 7, 30050))
			if err != nil || ev.Status != tc.want || ev.PaymentID != 7 || ev.ExternalID != "7" || ev.ExtAmount != tc.amount {
				t.Fatalf("%+v %v", ev, err)
			}
		})
	}
	if got := g.Ack(core.GatewayEvent{}); got != `{"code":0}` {
		t.Fatalf("ответ кассе: %s", got)
	}
}

func TestVerifyRejects(t *testing.T) {
	g := New(good(), "", nil)
	noSig := hook(t, "notify_pay.form", testSecret, 7, 30000)
	noSig.Header.Del("Content-HMAC")
	// Подпись по другим байтам тела (порядок полей тот же, но лишний символ) не проходит.
	tampered := hook(t, "notify_pay.form", testSecret, 7, 30000)
	tampered.Body = io.NopCloser(strings.NewReader(sample(t, "notify_pay.form", 7, 30000) + "&"))
	for name, r := range map[string]*http.Request{"без подписи": noSig, "тело изменено": tampered} {
		if _, err := g.Verify(r); err == nil {
			t.Errorf("%s: уведомление принято", name)
		}
	}
	if _, err := New(cfg{}, "", nil).Verify(hook(t, "notify_pay.form", "", 7, 30000)); err == nil {
		t.Error("пустой секрет подписал уведомление")
	}
	usd := strings.Replace(sample(t, "notify_pay.form", 7, 30000), "Currency=RUB", "Currency=USD", 1)
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(usd))
	r.Header.Set("Content-HMAC", sign([]byte(usd), testSecret))
	if _, err := g.Verify(r); err == nil {
		t.Error("оплата в долларах принята как рубли")
	}
}

func TestStatus(t *testing.T) {
	srv := fakeAPI(t, map[int64]int64{5: 15050})
	g := New(good(), srv.URL, srv.Client())
	ev, err := g.Status(t.Context(), core.Payment{ID: 5, ExternalID: "5"})
	if err != nil || ev.Status != core.PayPaid || ev.ExtAmount != 15050 || ev.PaymentID != 5 {
		t.Fatalf("оплата: %+v %v", ev, err)
	}
	ev, err = g.Status(t.Context(), core.Payment{ID: 6, ExternalID: "6"})
	if err != nil || ev.Status != core.PayPending {
		t.Fatalf("платежа ещё нет: %+v %v", ev, err)
	}
}

func TestSecretNotInError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, testSecret+testID, http.StatusInternalServerError)
	}))
	defer srv.Close()
	_, err := New(good(), srv.URL, srv.Client()).Status(t.Context(), core.Payment{ID: 1, ExternalID: "1"})
	if err == nil || strings.Contains(err.Error(), testSecret) || strings.Contains(err.Error(), testID) {
		t.Fatalf("ошибка: %v", err)
	}
}

func TestKopecks(t *testing.T) {
	for in, want := range map[string]int64{"300": 30000, "300.5": 30050, "300.00": 30000, "0.01": 1, "12.3400": 1234} {
		if got, err := kopecks(in); err != nil || got != want {
			t.Errorf("%s: %d %v", in, got, err)
		}
	}
	for _, bad := range []string{"", "1.005", "-1", "abc", "1e3"} {
		if _, err := kopecks(bad); err == nil {
			t.Errorf("%q принято", bad)
		}
	}
	if rubles(30000) != "300.00" || rubles(12305) != "123.05" {
		t.Error("rubles")
	}
}
