package kassaai

import (
	"encoding/json"
	"github.com/bini69-oi/worker/pay"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
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

var full = cfg{keyShop: "12345", keyAPI: "apikey", keySecret: "two", keyIP: "203.0.113.5"}

// fakeAPI изображает API: проверяет подпись HMAC и отдаёт записанный образец из testdata.
func fakeAPI(t *testing.T, files map[string]string) http.Handler {
	read := func(name string) []byte {
		raw, err := os.ReadFile("testdata/" + name)
		if err != nil {
			t.Error(err)
		}
		return raw
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in map[string]any
		dec := json.NewDecoder(r.Body)
		dec.UseNumber() // иначе большие числа станут float и подпись разойдётся
		if dec.Decode(&in) != nil {
			http.NotFound(w, r)
			return
		}
		if sig, _ := in["signature"].(string); sig != apiSign(in, "apikey") {
			_, _ = w.Write(read("create_error.json"))
			return
		}
		switch r.URL.Path {
		case "/orders/create":
			if in["ip"] != "203.0.113.5" || in["i"] == nil {
				t.Errorf("в заказе нет ip или способа: %v", in)
			}
			_, _ = w.Write(read("create_ok.json"))
		case "/orders":
			name, ok := files[in["paymentId"].(string)]
			if !ok {
				_, _ = w.Write([]byte(`{"type":"success","orders":[]}`))
				return
			}
			_, _ = w.Write(read(name))
		default:
			http.NotFound(w, r)
		}
	})
}

// hook собирает уведомление по образцу testdata/notification.form, подписывая его секретом secret.
func hook(t *testing.T, p core.Payment, amount, secret, ip string) *http.Request {
	t.Helper()
	raw, err := os.ReadFile("testdata/notification.form")
	if err != nil {
		t.Fatal(err)
	}
	form, _ := url.ParseQuery(strings.TrimSpace(string(raw)))
	form.Set("MERCHANT_ORDER_ID", strconv.FormatInt(p.ID, 10))
	form.Set("AMOUNT", amount)
	form.Set("SIGN", md5hex("12345:"+amount+":"+secret+":"+form.Get("MERCHANT_ORDER_ID")))
	r := httptest.NewRequest(http.MethodPost, "/pay/kassaai", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.RemoteAddr = ip + ":1234"
	return r
}

func TestSuite(t *testing.T) {
	srv := httptest.NewServer(fakeAPI(t, nil))
	defer srv.Close()
	gatewaytest.Run(t, gatewaytest.Case{
		Gateway:      New(full, srv.URL, srv.Client()),
		Unconfigured: New(cfg{}, srv.URL, srv.Client()),
		Amount:       30000,
		Paid:         func(p core.Payment, a int64) *http.Request { return hook(t, p, formatMoney(a), "two", "1.2.3.4") },
		Forged: func(p core.Payment, a int64) *http.Request {
			return hook(t, p, formatMoney(a), "чужой", "1.2.3.4")
		},
	})
}

func TestCreate(t *testing.T) {
	srv := httptest.NewServer(fakeAPI(t, nil))
	defer srv.Close()
	inv, err := New(full, srv.URL, srv.Client()).Create(t.Context(), core.Payment{ID: 7, Amount: 30050})
	if err != nil || inv.ExtAmount != 30050 || inv.ExternalID != "7" || !strings.HasPrefix(inv.URL, "https://pay.fk.money/") {
		t.Fatalf("счёт: %+v %v", inv, err)
	}
	bad := cfg{keyShop: "12345", keyAPI: "чужой", keySecret: "two", keyIP: "203.0.113.5"}
	if _, err := New(bad, srv.URL, srv.Client()).Create(t.Context(), core.Payment{ID: 7, Amount: 30000}); err == nil {
		t.Error("ошибка кассы принята за успех")
	}
}

func TestVerifyTable(t *testing.T) {
	_, office, _ := net.ParseCIDR("10.0.0.0/24")
	for _, tc := range []struct {
		name, amount, sec, ip string
		restrict, wantErr     bool
		wantCents             int64
	}{
		{name: "верная подпись", amount: "300", sec: "two", ip: "10.0.0.5", restrict: true, wantCents: 30000},
		{name: "сумма с копейками", amount: "300.5", sec: "two", ip: "10.0.0.5", wantCents: 30050},
		{name: "чужой IP", amount: "300", sec: "two", ip: "8.8.8.8", restrict: true, wantErr: true},
		{name: "чужая подпись", amount: "300", sec: "x", ip: "10.0.0.5", wantErr: true},
		{name: "доли копейки", amount: "300.001", sec: "two", ip: "10.0.0.5", wantErr: true},
		{name: "отрицательная", amount: "-5", sec: "two", ip: "10.0.0.5", wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := New(full, "", nil)
			if tc.restrict {
				g.RestrictIPs([]*net.IPNet{office})
			}
			ev, err := g.Verify(hook(t, core.Payment{ID: 7}, tc.amount, tc.sec, tc.ip))
			if (err != nil) != tc.wantErr {
				t.Fatalf("ошибка: %v", err)
			}
			if !tc.wantErr && (ev.ExtAmount != tc.wantCents || ev.PaymentID != 7 || ev.Status != core.PayPaid) {
				t.Fatalf("событие: %+v", ev)
			}
		})
	}
	if New(full, "", nil).Ack(core.GatewayEvent{}) != "YES" {
		t.Error("ответ кассе не YES")
	}
}

func TestStatus(t *testing.T) {
	srv := httptest.NewServer(fakeAPI(t, map[string]string{"7": "orders_paid.json", "8": "orders_new.json", "9": "orders_cancelled.json"}))
	defer srv.Close()
	g := New(full, srv.URL, srv.Client())
	for _, tc := range []struct {
		id     string
		status core.PayStatus
		cents  int64
	}{{"7", core.PayPaid, 30000}, {"8", core.PayPending, 15050}, {"9", core.PayFailed, 30000}} {
		ev, err := g.Status(t.Context(), core.Payment{ExternalID: tc.id})
		if err != nil || ev.Status != tc.status || ev.ExtAmount != tc.cents {
			t.Errorf("заказ %s: %+v %v", tc.id, ev, err)
		}
	}
	if _, err := g.Status(t.Context(), core.Payment{ExternalID: "404"}); err == nil {
		t.Error("несуществующий заказ принят")
	}
	if _, err := New(cfg{}, srv.URL, srv.Client()).Status(t.Context(), core.Payment{ExternalID: "7"}); err == nil {
		t.Error("статус без ключей")
	}
}

func TestKeysNotInError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "apikey", 500) }))
	defer srv.Close()
	_, err := New(full, srv.URL, srv.Client()).Status(t.Context(), core.Payment{ExternalID: "7"})
	if err == nil || strings.Contains(err.Error(), "apikey") {
		t.Fatalf("ошибка: %v", err)
	}
}

func TestMoney(t *testing.T) {
	for in, want := range map[string]int64{"300": 30000, "300.5": 30050, "0.01": 1, "12.3400": 1234} {
		if got, err := pay.Kopecks(in); err != nil || got != want {
			t.Errorf("%s: %d %v", in, got, err)
		}
	}
	for in, want := range map[int64]string{30000: "300", 30050: "300.5", 12305: "123.05", 10000: "100"} {
		if formatMoney(in) != want {
			t.Errorf("formatMoney(%d) = %s", in, formatMoney(in))
		}
	}
}
