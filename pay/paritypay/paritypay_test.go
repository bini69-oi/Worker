package paritypay

import (
	"encoding/hex"
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

type cfg map[string]string

func (c cfg) String(k string) string { return c[k] }
func (c cfg) Secret(k string) string { return c[k] }

var full = cfg{keyShop: "shop-test", keySecret: "key-one", keyCallback: "key-two", keyMethod: "SBP"}

var webhookTmpl = func() string {
	b, err := os.ReadFile("testdata/webhook.tmpl")
	if err != nil {
		panic(err)
	}
	return string(b)
}()

const extID = "0192f6d1-7a3c-7c1e-9b11-5e2a4d6f8a01"

func read(t testing.TB, name string) string {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// fakeAPI проверяет ключи и отдаёт записанные ответы. Сумма счёта 300 рублей.
func fakeAPI(t *testing.T, statusFile string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-ShopId") != "shop-test" || r.Header.Get("X-SecretKey") != "key-one" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(read(t, "create_error.json")))
			return
		}
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v2/invoice/create":
			raw, _ := readAll(r)
			if !strings.Contains(raw, `"amount":300.00`) || !strings.Contains(raw, `"service":"sbp"`) ||
				!strings.Contains(raw, `"order_id":"`+orderPrefix) || !strings.Contains(raw, `"callback_url":"https://hook.test/pay/paritypay"`) {
				t.Errorf("счёт создан не так: %s", raw)
			}
			_, _ = w.Write([]byte(read(t, "create_ok.json")))
		case r.Method == http.MethodGet && r.URL.Path == "/v2/invoice/status" && r.URL.Query().Get("id") == extID:
			_, _ = w.Write([]byte(strings.ReplaceAll(read(t, statusFile), "{{AMOUNT}}", "300.00")))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func readAll(r *http.Request) (string, error) {
	b, err := io.ReadAll(r.Body)
	return string(b), err
}

func newGW(srv *httptest.Server) *Gateway {
	g := New(full, srv.URL, srv.Client())
	g.Hook = "https://hook.test/pay/paritypay"
	return g
}

func signed(body, key string) *http.Request {
	fields, err := parseBody([]byte(body))
	if err != nil {
		panic(err)
	}
	r := httptest.NewRequest(http.MethodPost, "/pay/paritypay", strings.NewReader(body))
	r.Header.Set("X-SIGNATURE", hex.EncodeToString(sign(signPayload(fields), key)))
	return r
}

func hook(p core.Payment, amount int64, key string) *http.Request {
	body := strings.NewReplacer("{{ORDER}}", orderPrefix+strconv.FormatInt(p.ID, 10),
		"{{AMOUNT}}", rubles(amount)).Replace(webhookTmpl)
	return signed(body, key)
}

func TestSuite(t *testing.T) {
	srv := fakeAPI(t, "status_paid.json")
	gatewaytest.Run(t, gatewaytest.Case{
		Gateway:      newGW(srv),
		Unconfigured: New(cfg{}, srv.URL, srv.Client()),
		Paid:         func(p core.Payment, a int64) *http.Request { return hook(p, a, "key-two") },
		Forged:       func(p core.Payment, a int64) *http.Request { return hook(p, a, "чужой") },
		Amount:       30000,
	})
}

func TestStatus(t *testing.T) {
	for file, want := range map[string]core.PayStatus{
		"status_paid.json": core.PayPaid, "status_pending.json": core.PayPending, "status_refunded.json": core.PayRefunded,
	} {
		srv := fakeAPI(t, file)
		ev, err := newGW(srv).Status(t.Context(), core.Payment{ExternalID: extID})
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
	if _, err := newGW(srv).Verify(hook(core.Payment{ID: 1}, 30000, "key-two")); err == nil {
		t.Fatal("оплата принята без подтверждения API")
	}
}

// API недоступен: вебхук не принимается, касса повторит его позже.
func TestHookAPIDown(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "down", http.StatusServiceUnavailable) }))
	defer srv.Close()
	if _, err := newGW(srv).Verify(hook(core.Payment{ID: 1}, 30000, "key-two")); err == nil {
		t.Fatal("оплата принята при недоступном API")
	}
}

func TestBadSignatures(t *testing.T) {
	srv := fakeAPI(t, "status_paid.json")
	g := newGW(srv)
	body := `{"id":"` + extID + `","status":"PAID","amount":300.00}`
	noSig := httptest.NewRequest(http.MethodPost, "/pay/paritypay", strings.NewReader(body))
	for name, r := range map[string]*http.Request{
		"без подписи": noSig, "чужой ключ": signed(body, "x"), "ключ запросов вместо ключа подписи": signed(body, "key-one"),
	} {
		if _, err := g.Verify(r); err == nil {
			t.Errorf("%s: вебхук принят", name)
		}
	}
	if _, err := New(cfg{keyShop: "s"}, srv.URL, srv.Client()).Verify(signed(body, "")); err == nil {
		t.Error("пустой ключ принят")
	}
}

// Подпись строится по значениям в порядке ключей, числа как присланы.
func TestSignPayload(t *testing.T) {
	f, err := parseBody([]byte(`{"b":1200,"a":"x","d":null,"c":true,"e":1.50}`))
	if err != nil {
		t.Fatal(err)
	}
	if got := signPayload(f); got != "x1200true1.50" {
		t.Fatalf("payload %q", got)
	}
}

func TestSecretNotInError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "key-one key-two", http.StatusInternalServerError)
	}))
	defer srv.Close()
	_, err := newGW(srv).Status(t.Context(), core.Payment{ExternalID: "x"})
	if err == nil || strings.Contains(err.Error(), "key-") {
		t.Fatalf("ошибка: %v", err)
	}
}

func TestMoney(t *testing.T) {
	for in, want := range map[string]int64{"300": 30000, "300.5": 30050, "1250.00": 125000, "0.01": 1} {
		if got, err := kopecks(in); err != nil || got != want {
			t.Errorf("%q: %d %v", in, got, err)
		}
	}
	for _, in := range []string{"", "0", "300.505", "abc", "-1"} {
		if _, err := kopecks(in); err == nil {
			t.Errorf("%q принято", in)
		}
	}
	var m money
	if err := json.Unmarshal([]byte(`1500.5`), &m); err != nil || m != "1500.5" {
		t.Errorf("число: %q %v", m, err)
	}
}
