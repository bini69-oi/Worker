package jupiter

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/bini69-oi/worker/core"
	"github.com/bini69-oi/worker/pay"
	"github.com/bini69-oi/worker/pay/gatewaytest"
)

type cfg map[string]string

func (c cfg) String(k string) string { return c[k] }
func (c cfg) Secret(k string) string { return c[k] }

var full = cfg{keyToken: "tok-test", keySecret: "sec-test", keyMethod: "12"}

const extID = "7788001"

func read(t testing.TB, name string) string {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// mac считает подпись руками, независимо от кода кассы.
func mac(msg, key string) string {
	m := hmac.New(sha256.New, []byte(key))
	m.Write([]byte(msg))
	return hex.EncodeToString(m.Sum(nil))
}

// fakeAPI проверяет подпись запросов и отдаёт записанные ответы.
func fakeAPI(t *testing.T, statusFile string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)
		switch r.URL.Path {
		case "/p2p_payin_v2.1":
			amount, _ := req["amount"].(map[string]any)
			customer, _ := req["customer"].(map[string]any)
			order, _ := req["order_id"].(string)
			id, _ := customer["id"].(string)
			msg := "token=tok-testorder_id=" + order + "amount.value=300.00amount.currency=RUBcustomer.id=" + id + "redirect=false"
			if amount["value"] != "300.00" || req["signature"] != mac(msg, "sec-test") || req["method_id"] != "12" ||
				req["callback_url"] != "https://w.example/pay/jupiter" {
				_, _ = w.Write([]byte(read(t, "create_error.json")))
				return
			}
			_, _ = w.Write([]byte(read(t, "create_ok.json")))
		case "/p2p_status_v2.1":
			if req["transaction_id"] != extID || req["signature"] != mac("token=tok-testtransaction_id="+extID, "sec-test") {
				http.Error(w, "{}", http.StatusUnauthorized)
				return
			}
			_, _ = w.Write([]byte(strings.ReplaceAll(read(t, statusFile), "{{AMOUNT}}", "300.00")))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func newGateway(srv *httptest.Server) *Gateway {
	g := New(full, srv.URL, srv.Client())
	g.Hook = "https://w.example/pay/jupiter"
	return g
}

var webhookTmpl = func() string {
	b, err := os.ReadFile("testdata/webhook.tmpl")
	if err != nil {
		panic(err)
	}
	return string(b)
}()

// hook собирает вебхук с подписью по спецификации: token, transaction_id, order_id, amount, recalculated, status.type.
func hook(p core.Payment, kop int64, key string) *http.Request {
	val := rubles(kop)
	order := orderPrefix + strconv.FormatInt(p.ID, 10)
	msg := "token=tok-testtransaction_id=" + extID + "order_id=" + order + "amount.value=" + val +
		"amount.currency=RUBrecalculated=falsestatus.type=success"
	body := strings.NewReplacer("{{ORDER}}", order, "{{AMOUNT}}", val, "{{SIG}}", mac(msg, key)).Replace(webhookTmpl)
	return httptest.NewRequest(http.MethodPost, "/pay/jupiter", strings.NewReader(body))
}

func TestSuite(t *testing.T) {
	srv := fakeAPI(t, "status_paid.json")
	gatewaytest.Run(t, gatewaytest.Case{
		Gateway:      newGateway(srv),
		Unconfigured: New(cfg{}, srv.URL, srv.Client()),
		Paid:         func(p core.Payment, a int64) *http.Request { return hook(p, a, "sec-test") },
		Forged:       func(p core.Payment, a int64) *http.Request { return hook(p, a, "чужой") },
		Amount:       30000,
	})
}

func TestStatus(t *testing.T) {
	for file, want := range map[string]core.PayStatus{
		"status_paid.json": core.PayPaid, "status_processing.json": core.PayPending, "status_cancelled.json": core.PayFailed,
	} {
		srv := fakeAPI(t, file)
		ev, err := newGateway(srv).Status(t.Context(), core.Payment{ExternalID: extID})
		if err != nil || ev.Status != want {
			t.Fatalf("%s: %+v %v", file, ev, err)
		}
		if want == core.PayPaid && ev.ExtAmount != 30000 {
			t.Fatalf("сумма %d", ev.ExtAmount)
		}
	}
}

// Вебхук подписан ключом магазина: оплата принимается по нему, даже если API кассы молчит.
// API остаётся запасной сверкой (Status), а не условием зачисления.
func TestSignedHookWithoutAPI(t *testing.T) {
	srv := fakeAPI(t, "status_paid.json")
	g := newGateway(srv)
	srv.Close()
	ev, err := g.Verify(hook(core.Payment{ID: 1}, 30000, "sec-test"))
	if err != nil || ev.Status != core.PayPaid || ev.ExtAmount != 30000 || ev.PaymentID != 1 {
		t.Fatalf("подписанный вебхук: %+v %v", ev, err)
	}
}

func TestBadSignatures(t *testing.T) {
	srv := fakeAPI(t, "status_paid.json")
	g := newGateway(srv)
	for name, body := range map[string]string{
		"без подписи": `{"transaction_id":"7788001","order_id":"worker-1","status":{"type":"success"}}`,
		"не JSON":     `не json`,
		"не hex":      `{"transaction_id":"7788001","status":{"type":"success"},"signature":"zz"}`,
	} {
		_, err := g.Verify(httptest.NewRequest(http.MethodPost, "/pay/jupiter", strings.NewReader(body)))
		if err != pay.ErrBadSignature {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestSecretNotInError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "sec-test tok-test", 500) }))
	defer srv.Close()
	_, err := New(full, srv.URL, srv.Client()).Status(t.Context(), core.Payment{ExternalID: "x"})
	if err == nil || strings.Contains(err.Error(), "sec-test") || strings.Contains(err.Error(), "tok-test") {
		t.Fatalf("ошибка: %v", err)
	}
}

func TestCreateRejected(t *testing.T) {
	srv := fakeAPI(t, "status_paid.json")
	bad := cfg{keyToken: "tok-test", keySecret: "другой"}
	if _, err := New(bad, srv.URL, srv.Client()).Create(t.Context(), core.Payment{ID: 1, UserID: 2, Amount: 30000}); err == nil {
		t.Fatal("отказ кассы принят за счёт")
	}
}

func TestMoney(t *testing.T) {
	for in, want := range map[string]int64{"300": 30000, "300.5": 30050, "300.50": 30050, "0.01": 1} {
		if got, err := kopecks(in); err != nil || got != want {
			t.Errorf("%q: %d %v", in, got, err)
		}
	}
	for _, in := range []string{"", "abc", "0", "1.234", "-1"} {
		if _, err := kopecks(in); err == nil {
			t.Errorf("%q принято", in)
		}
	}
	if rubles(12305) != "123.05" {
		t.Error("rubles")
	}
}
