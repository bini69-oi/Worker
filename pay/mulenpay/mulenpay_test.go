package mulenpay

import (
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
func (c cfg) Int(k string) int64     { n, _ := strconv.ParseInt(c[k], 10, 64); return n }

var full = cfg{keyAPI: "key-1", keyShop: "77", keySecret: "sk-test"}

func read(t testing.TB, name string) string {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// fakeAPI отдаёт записанные ответы. Платёж 4821 помнит сумму и номер из счёта; статус берёт из файла.
func fakeAPI(t *testing.T, statusFile string) *httptest.Server {
	t.Helper()
	var uuid, amount string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer key-1" {
			http.Error(w, "no", http.StatusUnauthorized)
			return
		}
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v2/payments":
			var req map[string]any
			_ = json.NewDecoder(r.Body).Decode(&req)
			uuid, amount = req["uuid"].(string), req["amount"].(string)
			if req["sign"] != createSign("rub", amount, "77", "sk-test") || req["shopId"] != float64(77) {
				_, _ = w.Write([]byte(read(t, "create_error.json")))
				return
			}
			_, _ = w.Write([]byte(read(t, "create_ok.json")))
		case r.Method == http.MethodGet && r.URL.Path == "/v2/payments/4821":
			_, _ = w.Write([]byte(strings.NewReplacer("{{UUID}}", uuid, "{{AMOUNT}}", amount).Replace(read(t, statusFile))))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// hook собирает вебхук по записанному образцу; amount в теле числом, как шлёт касса.
// secret пустой: вебхук без подписи.
func hook(t *testing.T, p core.Payment, amount int64, secret string) *http.Request {
	t.Helper()
	body := strings.NewReplacer("{{UUID}}", uuidPrefix+strconv.FormatInt(p.ID, 10), "{{AMOUNT}}", pay.Rubles(amount)).
		Replace(read(t, "webhook.tmpl"))
	if secret != "" {
		raw, _, _, err := hookFields([]byte(body))
		if err != nil {
			t.Fatal(err)
		}
		body = strings.TrimSpace(body)
		body = strings.TrimSuffix(body, "}") + `,"sign":"` + hookSign(raw, secret) + `"}`
	}
	return httptest.NewRequest(http.MethodPost, "/pay/mulenpay", strings.NewReader(body))
}

func TestGateway(t *testing.T) {
	srv := fakeAPI(t, "status_paid.json")
	gatewaytest.Run(t, gatewaytest.Case{
		Gateway:      New(full, srv.URL, srv.Client()),
		Unconfigured: New(cfg{}, srv.URL, srv.Client()),
		Paid:         func(p core.Payment, a int64) *http.Request { return hook(t, p, a, "sk-test") },
		Forged:       func(p core.Payment, a int64) *http.Request { return hook(t, p, a, "чужой") },
		Amount:       30000,
	})
}

// create зовёт Create у фейкового API, чтобы тот запомнил платёж 4821.
func create(t *testing.T, g *Gateway) core.Payment {
	t.Helper()
	p := core.Payment{ID: 7, Amount: 30050}
	inv, err := g.Create(t.Context(), p)
	if err != nil || inv.ExternalID != "4821" || inv.URL == "" || inv.ExtAmount != 30050 {
		t.Fatalf("счёт: %+v %v", inv, err)
	}
	p.ExternalID = inv.ExternalID
	return p
}

func TestUnsignedHookAsksAPI(t *testing.T) {
	srv := fakeAPI(t, "status_pending.json")
	g := New(full, srv.URL, srv.Client())
	p := create(t, g)
	// Вебхук без подписи говорит «оплачено», а API говорит «в обработке»: верим API.
	ev, err := g.Verify(hook(t, p, 30050, ""))
	if err != nil || ev.Status != core.PayPending || ev.PaymentID != 7 {
		t.Fatalf("без подписи при статусе API «в обработке»: %+v %v", ev, err)
	}
	paid := fakeAPI(t, "status_paid.json")
	g = New(full, paid.URL, paid.Client())
	p = create(t, g)
	ev, err = g.Verify(hook(t, p, 30050, ""))
	if err != nil || ev.Status != core.PayPaid || ev.ExtAmount != 30050 {
		t.Fatalf("без подписи при статусе API «оплачен»: %+v %v", ev, err)
	}
	// Сумма вебхука другая: наружу уходит она, ядро отклонит.
	if ev, _ = g.Verify(hook(t, p, 100, "")); ev.ExtAmount != 100 {
		t.Fatalf("сумма вебхука потеряна: %+v", ev)
	}
}

func TestStatus(t *testing.T) {
	srv := fakeAPI(t, "status_paid.json")
	g := New(full, srv.URL, srv.Client())
	p := create(t, g)
	ev, err := g.Status(t.Context(), p)
	if err != nil || ev.Status != core.PayPaid || ev.ExtAmount != 30050 {
		t.Fatalf("оплата: %+v %v", ev, err)
	}
	if _, err := g.Status(t.Context(), core.Payment{ExternalID: "999"}); err == nil {
		t.Fatal("неизвестный платёж принят")
	}
}

func TestStatusMapAndAmounts(t *testing.T) {
	for in, want := range map[string]core.PayStatus{"0": core.PayPending, "1": core.PayPending, "5": core.PayPending,
		"2": core.PayFailed, "4": core.PayFailed, "3": core.PayPaid, "success": core.PayPaid, "cancel": core.PayFailed} {
		if mapStatus(in) != want {
			t.Errorf("статус %s", in)
		}
	}
	for in, want := range map[string]int64{"300": 30000, "300.5": 30050, "0.01": 1, "12.3400": 1234} {
		if got, err := pay.Kopecks(in); err != nil || got != want {
			t.Errorf("%s: %d %v", in, got, err)
		}
	}
	for _, in := range []string{"", "abc", "1.005", "-1"} {
		if _, err := pay.Kopecks(in); err == nil {
			t.Errorf("%q: ждали ошибку", in)
		}
	}
	if pay.Rubles(30050) != "300.50" || pay.Rubles(5) != "0.05" {
		t.Fatal("rubles")
	}
}

func TestSignPythonStyle(t *testing.T) {
	// Касса могла считать подпись по str() Python: 300.0 вместо 300.00.
	body := `{"id":1,"amount":300.00,"ok":true,"status":"success"}`
	raw, py, _, err := hookFields([]byte(body))
	if err != nil || raw != "1300.00truesuccess" || py != "1300.0Truesuccess" {
		t.Fatalf("%q %q %v", raw, py, err)
	}
	if !signOK(raw, py, hookSign(py, "k"), "k") || signOK(raw, py, hookSign(py, "k"), "z") || signOK(raw, py, "", "k") {
		t.Fatal("подпись")
	}
}

func TestSecretNotInError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "key-1 sk-test", 500) }))
	defer srv.Close()
	g := New(full, srv.URL, srv.Client())
	_, err := g.Status(t.Context(), core.Payment{ExternalID: "1"})
	if err == nil || strings.Contains(err.Error(), "key-1") || strings.Contains(err.Error(), "sk-test") {
		t.Fatalf("ошибка: %v", err)
	}
}
