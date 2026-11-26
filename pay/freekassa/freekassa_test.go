package freekassa

import (
	"encoding/json"
	"github.com/bini69-oi/worker/pay"
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

func (c cfg) Secret(k string) string { return c[k] }

var full = cfg{"freekassa.shop_id": "12345", "freekassa.secret1": "one", "freekassa.secret2": "two",
	"freekassa.api_key": "apikey"}

// fakeAPI изображает POST /orders: проверяет подпись HMAC и отдаёт записанный образец из testdata по номеру заказа.
func fakeAPI(t *testing.T, files map[string]string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in map[string]any
		dec := json.NewDecoder(r.Body)
		dec.UseNumber() // иначе большие числа станут float и подпись разойдётся
		if r.URL.Path != "/orders" || dec.Decode(&in) != nil {
			http.NotFound(w, r)
			return
		}
		sig, _ := in["signature"].(string)
		if sig != apiSign(in, "apikey") {
			http.Error(w, `{"type":"error"}`, http.StatusUnauthorized)
			return
		}
		name, ok := files[in["paymentId"].(string)]
		if !ok {
			_, _ = w.Write([]byte(`{"type":"success","orders":[]}`))
			return
		}
		raw, err := os.ReadFile("testdata/" + name)
		if err != nil {
			t.Error(err)
		}
		_, _ = w.Write(raw)
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
	r := httptest.NewRequest(http.MethodPost, "/pay/freekassa", strings.NewReader(form.Encode()))
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
		Paid: func(p core.Payment, a int64) *http.Request {
			return hook(t, p, formatMoney(a), "two", "1.2.3.4")
		},
		Forged: func(p core.Payment, a int64) *http.Request {
			return hook(t, p, formatMoney(a), "чужой", "1.2.3.4")
		},
	})
}

func TestVerifyTable(t *testing.T) {
	good := OfficialNets()[0].IP.String()
	for _, tc := range []struct {
		name         string
		amount, sec  string
		signAmount   string // сумма, под которую посчитана подпись, пусто: та же
		ip           string
		restrict     bool
		wantErr      bool
		wantCents    int64
		wantPaymentI int64
	}{
		{name: "верная подпись", amount: "300", sec: "two", ip: good, restrict: true, wantCents: 30000, wantPaymentI: 7},
		{name: "сумма с копейками", amount: "300.50", sec: "two", ip: good, restrict: true, wantCents: 30050, wantPaymentI: 7},
		{name: "чужой IP", amount: "300", sec: "two", ip: "8.8.8.8", restrict: true, wantErr: true},
		{name: "чужой IP без фильтра", amount: "300", sec: "two", ip: "8.8.8.8", wantCents: 30000, wantPaymentI: 7},
		{name: "чужая подпись", amount: "300", sec: "x", ip: good, restrict: true, wantErr: true},
		{name: "секрет 1 вместо 2", amount: "300", sec: "one", ip: good, wantErr: true},
		{name: "сумма подменена", amount: "1", signAmount: "300", sec: "two", ip: good, wantErr: true},
		{name: "доли копейки", amount: "300.001", sec: "two", ip: good, wantErr: true},
		{name: "отрицательная", amount: "-5", sec: "two", ip: good, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := New(full, "", nil)
			if tc.restrict {
				g.RestrictIPs(OfficialNets())
			}
			r := hook(t, core.Payment{ID: 7}, tc.amount, tc.sec, tc.ip)
			if tc.signAmount != "" {
				r = hook(t, core.Payment{ID: 7}, tc.signAmount, tc.sec, tc.ip)
				f, _ := url.ParseQuery(readBody(t, r))
				f.Set("AMOUNT", tc.amount)
				r = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(f.Encode()))
				r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			}
			ev, err := g.Verify(r)
			if (err != nil) != tc.wantErr {
				t.Fatalf("ошибка: %v", err)
			}
			if !tc.wantErr && (ev.ExtAmount != tc.wantCents || ev.PaymentID != tc.wantPaymentI || ev.Status != core.PayPaid) {
				t.Fatalf("событие: %+v", ev)
			}
		})
	}
}

func readBody(t *testing.T, r *http.Request) string {
	t.Helper()
	b := new(strings.Builder)
	buf := make([]byte, 4096)
	n, _ := r.Body.Read(buf)
	b.Write(buf[:n])
	return b.String()
}

func TestCreateLink(t *testing.T) {
	inv, err := New(full, "", nil).Create(t.Context(), core.Payment{ID: 7, Amount: 30050})
	if err != nil || inv.ExtAmount != 30050 || inv.ExternalID != "7" {
		t.Fatalf("счёт: %+v %v", inv, err)
	}
	u, _ := url.Parse(inv.URL)
	q := u.Query()
	if u.Host != "pay.fk.money" || q.Get("oa") != "300.50" || q.Get("s") != md5hex("12345:300.50:one:RUB:7") {
		t.Fatalf("ссылка: %s", inv.URL)
	}
	if strings.Contains(inv.URL, "two") {
		t.Fatal("в ссылке секрет 2")
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
	noKey := cfg{"freekassa.shop_id": "12345"}
	if _, err := New(noKey, srv.URL, srv.Client()).Status(t.Context(), core.Payment{ExternalID: "7"}); err == nil {
		t.Error("статус без API-ключа")
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
	if formatMoney(30000) != "300" || formatMoney(12305) != "123.05" {
		t.Error("formatMoney")
	}
}
