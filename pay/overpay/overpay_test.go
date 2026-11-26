package overpay

import (
	"encoding/json"
	"github.com/bini69-oi/worker/pay"
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
func (c cfg) Int(k string) int64     { n, _ := strconv.ParseInt(c[k], 10, 64); return n }

var full = cfg{keyUser: "user-test", keyPass: "pass-test", keyProject: "77", keyMethods: "card, fps", keyLife: "60"}

func read(t testing.TB, name string) string {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// api это фейковый API: проверяет Basic-авторизацию и отдаёт записанные ответы.
// reported это сумма, которую API называет в ответе о заказе; order это номер платежа из последнего созданного счёта.
type api struct {
	t          *testing.T
	statusFile string
	mu         sync.Mutex
	reported   string
	order      string
}

func (a *api) set(amount int64) {
	a.mu.Lock()
	a.reported = pay.Rubles(amount)
	a.mu.Unlock()
}

func (a *api) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if u, p, ok := r.BasicAuth(); !ok || u != "user-test" || p != "pass-test" {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(read(a.t, "create_error.json")))
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	switch {
	case r.Method == http.MethodPost && r.URL.Path == "/orders/":
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)
		id, _ := req["merchantTransactionId"].(string)
		methods, _ := req["paymentMethods"].([]any)
		if req["amount"] != "300.00" || req["currency"] != "RUB" || req["projectId"] != "77" ||
			!strings.HasPrefix(id, orderPrefix) || len(methods) != 2 || req["livetimeMinutes"] != float64(60) {
			a.t.Errorf("заказ создан не так: %v", req)
		}
		a.order = id
		_, _ = w.Write([]byte(strings.ReplaceAll(read(a.t, "create_ok.json"), "worker-1", id)))
	case r.Method == http.MethodGet && r.URL.Path == "/orders/8841203":
		out := strings.ReplaceAll(read(a.t, a.statusFile), "{{AMOUNT}}", a.reported)
		_, _ = w.Write([]byte(strings.ReplaceAll(out, "worker-1", a.order)))
	default:
		http.NotFound(w, r)
	}
}

func newAPI(t *testing.T, statusFile string) (*api, *httptest.Server) {
	t.Helper()
	a := &api{t: t, statusFile: statusFile, reported: "300.00", order: "worker-1"}
	srv := httptest.NewServer(a)
	t.Cleanup(srv.Close)
	return a, srv
}

func hook(p core.Payment, id string) *http.Request {
	body := strings.NewReplacer("{{ID}}", id, "{{ORDER}}", orderPrefix+strconv.FormatInt(p.ID, 10),
		"{{AMOUNT}}", "300.00").Replace(mustRead("webhook.tmpl"))
	return httptest.NewRequest(http.MethodPost, "/pay/overpay", strings.NewReader(body))
}

func TestSuite(t *testing.T) {
	a, srv := newAPI(t, "status_charged.json")
	gatewaytest.Run(t, gatewaytest.Case{
		Gateway:      New(full, srv.URL, srv.Client()),
		Unconfigured: New(cfg{}, srv.URL, srv.Client()),
		// Вебхук не подписан: «оплачено на сумму» значит, что API так ответит.
		Paid: func(p core.Payment, amount int64) *http.Request {
			a.set(amount)
			return hook(p, "8841203")
		},
		// Поддельный вебхук: номер заказа, которого API не знает.
		Forged: func(p core.Payment, _ int64) *http.Request { return hook(p, "999") },
		Amount: 30000,
	})
}

func TestStatus(t *testing.T) {
	for file, want := range map[string]core.PayStatus{
		"status_charged.json": core.PayPaid, "status_new.json": core.PayPending, "status_refunded.json": core.PayRefunded,
	} {
		_, srv := newAPI(t, file)
		ev, err := New(full, srv.URL, srv.Client()).Status(t.Context(), core.Payment{ExternalID: "8841203"})
		if err != nil || ev.Status != want {
			t.Fatalf("%s: %+v %v", file, ev, err)
		}
		if want == core.PayPaid && ev.ExtAmount != 30000 {
			t.Fatalf("сумма %d", ev.ExtAmount)
		}
	}
}

// API недоступен: вебхук отклонён, деньги не зачисляются (fail-closed).
func TestAPIDown(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close()
	g := New(full, url, nil)
	if _, err := g.Verify(hook(core.Payment{ID: 1}, "8841203")); err == nil {
		t.Fatal("вебхук принят при недоступном API")
	}
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "x", 500) }))
	defer bad.Close()
	if _, err := New(full, bad.URL, bad.Client()).Verify(hook(core.Payment{ID: 1}, "8841203")); err == nil {
		t.Fatal("вебхук принят при ошибке API")
	}
}

// Вебхук говорит «оплачено», API говорит «новый»: верим API, оплаты нет.
func TestHookNotConfirmed(t *testing.T) {
	_, srv := newAPI(t, "status_new.json")
	ev, err := New(full, srv.URL, srv.Client()).Verify(hook(core.Payment{ID: 1}, "8841203"))
	if err != nil || ev.Status != core.PayPending {
		t.Fatalf("%+v %v", ev, err)
	}
}

// Вебхук про заказ одного платежа, а API отвечает про другой.
func TestOrderMismatch(t *testing.T) {
	a, srv := newAPI(t, "status_charged.json")
	a.order = "worker-5"
	if _, err := New(full, srv.URL, srv.Client()).Verify(hook(core.Payment{ID: 1}, "8841203")); err == nil {
		t.Fatal("чужой заказ принят")
	}
}

func TestBadWebhook(t *testing.T) {
	_, srv := newAPI(t, "status_charged.json")
	g := New(full, srv.URL, srv.Client())
	for _, body := range []string{"", "не json", `{"status":"charged"}`} {
		r := httptest.NewRequest(http.MethodPost, "/pay/overpay", strings.NewReader(body))
		if _, err := g.Verify(r); err == nil {
			t.Errorf("%q принят", body)
		}
	}
}

func TestSecretNotInError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "pass-test", 500) }))
	defer srv.Close()
	_, err := New(full, srv.URL, srv.Client()).Status(t.Context(), core.Payment{ExternalID: "x"})
	if err == nil || strings.Contains(err.Error(), "pass-test") || strings.Contains(err.Error(), "user-test") {
		t.Fatalf("ошибка: %v", err)
	}
}

// Битая пара сертификат и ключ: запрос не уходит, секрет не в ошибке.
func TestBadCert(t *testing.T) {
	c := cfg{keyUser: "u", keyPass: "p", keyProject: "1", keyCert: "SECRET-CERT", keyKey: "SECRET-KEY"}
	_, err := New(c, "http://127.0.0.1:1", nil).Status(t.Context(), core.Payment{ExternalID: "x"})
	if err == nil || strings.Contains(err.Error(), "SECRET") {
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
