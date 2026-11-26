package aurapay

import (
	"context"
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
	"github.com/bini69-oi/worker/pay"
	"github.com/bini69-oi/worker/pay/gatewaytest"
)

type cfg map[string]string

func (c cfg) String(k string) string { return c[k] }
func (c cfg) Secret(k string) string { return c[k] }

var full = cfg{keyAPI: "ak-test", keyShop: "shop-1", keySecret: "sec-test", keyURL: "https://bot.example/pay/aurapay"}

func read(t testing.TB, name string) string {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// fakeAPI проверяет заголовки и отдаёт записанные ответы.
func fakeAPI(t *testing.T, statusFile string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-ApiKey") != "ak-test" || r.Header.Get("X-ShopId") != "shop-1" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(read(t, "create_error.json")))
			return
		}
		body, _ := io.ReadAll(r.Body)
		var req map[string]any
		_ = json.Unmarshal(body, &req)
		switch r.URL.Path {
		case "/invoice/create":
			if req["callback_url"] != "https://bot.example/pay/aurapay" || req["service"] != "sbp" ||
				!strings.HasPrefix(req["order_id"].(string), orderPrefix) {
				t.Errorf("счёт создан не так: %s", body)
			}
			if _, ok := req["amount"].(float64); !ok {
				t.Errorf("amount должен быть числом: %s", body)
			}
			_, _ = w.Write([]byte(strings.ReplaceAll(read(t, "create_ok.json"), "{{ORDER}}", req["order_id"].(string))))
		case "/invoice/status":
			if req["id"] != "inv-7f3a" {
				t.Errorf("статус спрошен не по id: %s", body)
			}
			out := strings.NewReplacer("{{ORDER}}", orderPrefix+"1", "{{AMOUNT}}", "300.00").Replace(read(t, statusFile))
			_, _ = w.Write([]byte(out))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// hook собирает вебхук и подписывает его склейкой значений в порядке ключей.
// Для записанного образца это amount, custom_fields, id, order_id, paid, service, status.
func hook(p core.Payment, amount int64, key, status string) *http.Request {
	order := orderPrefix + strconv.FormatInt(p.ID, 10)
	body := strings.NewReplacer("{{ORDER}}", order, "{{AMOUNT}}", rubles(amount), "{{STATUS}}", status).
		Replace(mustRead("webhook.tmpl"))
	msg := rubles(amount) + "inv-7f3a" + order + "True" + "sbp" + status
	r := httptest.NewRequest(http.MethodPost, "/pay/aurapay", strings.NewReader(body))
	r.Header.Set("X-SIGNATURE", hexSign(msg, key))
	return r
}

func hexSign(msg, key string) string {
	return hex.EncodeToString(sign([]byte(msg), key))
}

func TestSuite(t *testing.T) {
	srv := fakeAPI(t, "status_paid.json")
	gatewaytest.Run(t, gatewaytest.Case{
		Gateway:      New(full, srv.URL, srv.Client()),
		Unconfigured: New(cfg{}, srv.URL, srv.Client()),
		Amount:       30000,
		Paid:         func(p core.Payment, a int64) *http.Request { return hook(p, a, "sec-test", "PAID") },
		Forged:       func(p core.Payment, a int64) *http.Request { return hook(p, a, "чужой-ключ", "PAID") },
	})
}

func TestCreate(t *testing.T) {
	srv := fakeAPI(t, "status_paid.json")
	inv, err := New(full, srv.URL, srv.Client()).Create(context.Background(), core.Payment{ID: 1, Amount: 30050})
	if err != nil {
		t.Fatal(err)
	}
	if inv.ExternalID != "inv-7f3a" || !strings.HasPrefix(inv.URL, "https://pay.aurapay.test/") || inv.ExtAmount != 30050 {
		t.Fatalf("счёт не тот: %+v", inv)
	}
}

func TestCreateWrongKey(t *testing.T) {
	srv := fakeAPI(t, "status_paid.json")
	bad := cfg{keyAPI: "ak-wrong", keyShop: "shop-1", keySecret: "sec-test"}
	_, err := New(bad, srv.URL, srv.Client()).Create(context.Background(), core.Payment{ID: 1, Amount: 30000})
	if err == nil || strings.Contains(err.Error(), "ak-wrong") {
		t.Fatalf("ждали ошибку без ключа, получили %v", err)
	}
}

func TestStatus(t *testing.T) {
	for _, tc := range []struct {
		file   string
		status core.PayStatus
		amount int64
	}{
		{"status_paid.json", core.PayPaid, 30000},
		{"status_pending.json", core.PayPending, 0},
	} {
		srv := fakeAPI(t, tc.file)
		ev, err := New(full, srv.URL, srv.Client()).Status(context.Background(), core.Payment{ID: 1, ExternalID: "inv-7f3a"})
		if err != nil || ev.Status != tc.status || ev.ExtAmount != tc.amount {
			t.Fatalf("%s: %+v %v", tc.file, ev, err)
		}
	}
}

func TestVerify(t *testing.T) {
	g := New(full, "", nil)
	ev, err := g.Verify(hook(core.Payment{ID: 5}, 30000, "sec-test", "PAID"))
	if err != nil || ev.PaymentID != 5 || ev.ExternalID != "inv-7f3a" || ev.ExtAmount != 30000 || ev.Status != core.PayPaid {
		t.Fatalf("%+v %v", ev, err)
	}
	ev, err = g.Verify(hook(core.Payment{ID: 5}, 30000, "sec-test", "EXPIRED"))
	if err != nil || ev.Status != core.PayFailed {
		t.Fatalf("EXPIRED: %+v %v", ev, err)
	}
	r := hook(core.Payment{ID: 5}, 30000, "sec-test", "PAID")
	r.Header.Del("X-SIGNATURE")
	if _, err := g.Verify(r); err != pay.ErrBadSignature {
		t.Fatalf("без подписи: %v", err)
	}
	if _, err := New(cfg{}, "", nil).Verify(hook(core.Payment{ID: 5}, 30000, "", "PAID")); err != pay.ErrBadSignature {
		t.Fatalf("без ключа: %v", err)
	}
	// подмена суммы при той же подписи
	r = hook(core.Payment{ID: 5}, 30000, "sec-test", "PAID")
	r2 := httptest.NewRequest(http.MethodPost, "/pay/aurapay", strings.NewReader(strings.Replace(mustRead("webhook.tmpl"),
		"{{AMOUNT}}", "1.00", 1)))
	r2.Header.Set("X-SIGNATURE", r.Header.Get("X-SIGNATURE"))
	if _, err := g.Verify(r2); err != pay.ErrBadSignature {
		t.Fatalf("подмена тела: %v", err)
	}
	for _, bad := range []string{"", "[]", `{"a":{"b":1}}`} {
		r := httptest.NewRequest(http.MethodPost, "/pay/aurapay", strings.NewReader(bad))
		r.Header.Set("X-SIGNATURE", "00")
		if _, err := g.Verify(r); err != pay.ErrBadSignature {
			t.Fatalf("%q: %v", bad, err)
		}
	}
}

func TestMoney(t *testing.T) {
	for in, want := range map[string]int64{"300": 30000, "300.5": 30050, "1250.00": 125000, "0.01": 1} {
		if got, err := kopecks(in); err != nil || got != want {
			t.Errorf("%q: %d %v", in, got, err)
		}
	}
	for _, in := range []string{"", "abc", "1.005", "-1", "1e2"} {
		if _, err := kopecks(in); err == nil {
			t.Errorf("%q: ждали ошибку", in)
		}
	}
	if rubles(30050) != "300.50" || rubles(5) != "0.05" {
		t.Fatal("rubles")
	}
}

func mustRead(name string) string {
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		panic(err)
	}
	return string(b)
}
