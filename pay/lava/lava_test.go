package lava

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

var full = cfg{keyShop: "shop-1", keySecret: "sk-test", keyHook: "hk-test", keyURL: "https://bot.example/pay/lava"}

func read(t testing.TB, name string) string {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// fakeAPI проверяет подпись запроса и отдаёт записанные ответы.
func fakeAPI(t *testing.T, statusFile string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if r.Header.Get("Signature") != hex.EncodeToString(sign(body, "sk-test")) {
			_, _ = w.Write([]byte(read(t, "create_error.json")))
			return
		}
		var req map[string]any
		_ = json.Unmarshal(body, &req)
		if req["shopId"] != "shop-1" {
			t.Errorf("не тот shopId: %v", req["shopId"])
		}
		switch r.URL.Path {
		case "/business/invoice/create":
			if req["hookUrl"] != "https://bot.example/pay/lava" || !strings.HasPrefix(req["orderId"].(string), orderPrefix) {
				t.Errorf("счёт создан не так: %s", body)
			}
			if _, ok := req["sum"].(float64); !ok || strings.Contains(string(body), `"sum":"`) {
				t.Errorf("sum должен быть числом: %s", body)
			}
			_, _ = w.Write([]byte(read(t, "create_ok.json")))
		case "/business/invoice/status":
			out := strings.NewReplacer("{{ORDER}}", orderPrefix+"1", "{{AMOUNT}}", "300.00").Replace(read(t, statusFile))
			_, _ = w.Write([]byte(out))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func hook(p core.Payment, amount int64, key, status string) *http.Request {
	body := strings.NewReplacer("{{ORDER}}", orderPrefix+strconv.FormatInt(p.ID, 10),
		"{{AMOUNT}}", pay.Rubles(amount), "{{STATUS}}", status).Replace(mustRead("webhook.tmpl"))
	r := httptest.NewRequest(http.MethodPost, "/pay/lava", strings.NewReader(body))
	r.Header.Set("Authorization", hex.EncodeToString(sign([]byte(body), key)))
	return r
}

func TestSuite(t *testing.T) {
	srv := fakeAPI(t, "status_paid.json")
	gatewaytest.Run(t, gatewaytest.Case{
		Gateway:      New(full, srv.URL, srv.Client()),
		Unconfigured: New(cfg{}, srv.URL, srv.Client()),
		Amount:       30000,
		Paid:         func(p core.Payment, a int64) *http.Request { return hook(p, a, "hk-test", "success") },
		Forged:       func(p core.Payment, a int64) *http.Request { return hook(p, a, "чужой-ключ", "success") },
	})
}

func TestCreate(t *testing.T) {
	srv := fakeAPI(t, "status_paid.json")
	g := New(full, srv.URL, srv.Client())
	inv, err := g.Create(context.Background(), core.Payment{ID: 1, Amount: 30050})
	if err != nil {
		t.Fatal(err)
	}
	if inv.ExternalID != "inv-7f3a" || !strings.HasPrefix(inv.URL, "https://pay.lava.test/") || inv.ExtCurrency != "RUB" {
		t.Fatalf("счёт не тот: %+v", inv)
	}
}

func TestCreateWrongKey(t *testing.T) {
	srv := fakeAPI(t, "status_paid.json")
	bad := cfg{keyShop: "shop-1", keySecret: "sk-wrong", keyHook: "hk-test"}
	_, err := New(bad, srv.URL, srv.Client()).Create(context.Background(), core.Payment{ID: 1, Amount: 30000})
	if err == nil || strings.Contains(err.Error(), "sk-wrong") {
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

func TestVerifyExactBytes(t *testing.T) {
	g := New(full, "", nil)
	ev, err := g.Verify(hook(core.Payment{ID: 5}, 30000, "hk-test", "success"))
	if err != nil || ev.PaymentID != 5 || ev.ExternalID != "inv-7f3a" || ev.ExtAmount != 30000 || ev.Status != core.PayPaid {
		t.Fatalf("%+v %v", ev, err)
	}
	// тело изменено после подписи: пересобранный JSON не спасает
	r := hook(core.Payment{ID: 5}, 30000, "hk-test", "success")
	sig := r.Header.Get("Authorization")
	body, _ := io.ReadAll(r.Body)
	r2 := httptest.NewRequest(http.MethodPost, "/pay/lava", strings.NewReader(strings.ReplaceAll(string(body), "  ", " ")))
	r2.Header.Set("Authorization", sig)
	if _, err := g.Verify(r2); err != pay.ErrBadSignature {
		t.Fatalf("ждали ErrBadSignature, получили %v", err)
	}
}

func TestVerifyEdge(t *testing.T) {
	g := New(full, "", nil)
	r := hook(core.Payment{ID: 5}, 30000, "hk-test", "success")
	r.Header.Del("Authorization")
	if _, err := g.Verify(r); err != pay.ErrBadSignature {
		t.Fatalf("без подписи: %v", err)
	}
	if _, err := New(cfg{}, "", nil).Verify(hook(core.Payment{ID: 5}, 30000, "", "success")); err != pay.ErrBadSignature {
		t.Fatalf("без ключа: %v", err)
	}
	ev, err := g.Verify(hook(core.Payment{ID: 5}, 30000, "hk-test", "expired"))
	if err != nil || ev.Status != core.PayFailed {
		t.Fatalf("expired: %+v %v", ev, err)
	}
}

func TestMoney(t *testing.T) {
	for in, want := range map[string]int64{"300": 30000, "300.5": 30050, "300.00": 30000, "0.01": 1} {
		if got, err := pay.Kopecks(in); err != nil || got != want {
			t.Errorf("%q: %d %v", in, got, err)
		}
	}
	for _, in := range []string{"", "abc", "1.005", "-1", "1e2"} {
		if _, err := pay.Kopecks(in); err == nil {
			t.Errorf("%q: ждали ошибку", in)
		}
	}
	if pay.Rubles(30050) != "300.50" || pay.Rubles(5) != "0.05" {
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
