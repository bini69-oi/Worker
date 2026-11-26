package stripe

import (
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

const (
	testKey     = "sk_test_секрет"
	testHook    = "whsec_test"
	testSession = "cs_test_a1B2c3D4e5F6g7H8i9J0"
)

type cfg map[string]string

func (c cfg) String(k string) string { return c[k] }
func (c cfg) Secret(k string) string { return c[k] }
func (c cfg) Int(k string) int64 {
	n, _ := strconv.ParseInt(c[k], 10, 64)
	return n
}

func good() cfg {
	return cfg{keySecret: testKey, keyWebhook: testHook, keyCurrency: "USD", keyRate: "9500"}
}

func data(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// newGateway собирает кассу с адресом возврата и фиксированными часами.
func newGateway(c cfg, url string) *Gateway {
	g := New(c, url, nil)
	g.Return = func(id int64) string { return "https://t.me/bot?start=paid_" + strconv.FormatInt(id, 10) }
	g.now = func() time.Time { return time.Unix(1700000000, 0) }
	return g
}

// fakeAPI изображает API Stripe: проверяет ключ и поля формы, отдаёт записанные ответы.
func fakeAPI(t *testing.T) *httptest.Server {
	t.Helper()
	create, paid, denied := data(t, "create_ok.json"), data(t, "session_paid.json"), data(t, "error_401.json")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+testKey {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write(denied)
			return
		}
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/checkout/sessions":
			_ = r.ParseForm()
			want := map[string]string{
				"mode": "payment", "line_items[0][quantity]": "1",
				"line_items[0][price_data][currency]":    "usd",
				"line_items[0][price_data][unit_amount]": "316",
				"metadata[payment_id]":                   r.PostForm.Get("client_reference_id"),
			}
			for k, v := range want {
				if r.PostForm.Get(k) != v {
					t.Errorf("поле %s = %q, ждали %q", k, r.PostForm.Get(k), v)
				}
			}
			if !strings.HasPrefix(r.PostForm.Get("success_url"), "https://t.me/") || r.PostForm.Get("cancel_url") == "" {
				t.Errorf("нет адресов возврата: %v", r.PostForm)
			}
			_, _ = w.Write(create)
		case r.Method == http.MethodGet && r.URL.Path == "/v1/checkout/sessions/"+testSession:
			_, _ = w.Write(paid)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// hook собирает подписанный вебхук из шаблона в testdata.
func hook(t *testing.T, tmpl string, p core.Payment, amount int64, secret string, vars ...string) *http.Request {
	t.Helper()
	pairs := append([]string{"{{ID}}", p.ExternalID, "{{ORDER}}", strconv.FormatInt(p.ID, 10),
		"{{AMOUNT}}", strconv.FormatInt(amount, 10)}, vars...)
	body := strings.NewReplacer(pairs...).Replace(string(data(t, tmpl)))
	ts := "1700000000"
	r := httptest.NewRequest(http.MethodPost, "/pay/stripe", strings.NewReader(body))
	r.Header.Set("Stripe-Signature", "t="+ts+",v1="+hexOf(sign(ts, []byte(body), secret)))
	return r
}

func hexOf(b []byte) string {
	const d = "0123456789abcdef"
	out := make([]byte, 0, len(b)*2)
	for _, c := range b {
		out = append(out, d[c>>4], d[c&15])
	}
	return string(out)
}

func paidHook(t *testing.T, secret string) func(core.Payment, int64) *http.Request {
	return func(p core.Payment, a int64) *http.Request {
		return hook(t, "event_session.tmpl", p, a, secret, "{{TYPE}}", "checkout.session.completed", "{{STATUS}}", "paid")
	}
}

func TestGateway(t *testing.T) {
	srv := fakeAPI(t)
	gatewaytest.Run(t, gatewaytest.Case{
		Gateway:      newGateway(good(), srv.URL),
		Unconfigured: newGateway(cfg{}, srv.URL),
		Paid:         paidHook(t, testHook),
		Forged:       paidHook(t, "whsec_чужой"),
		Amount:       30000,
	})
}

func TestCreate(t *testing.T) {
	srv := fakeAPI(t)
	inv, err := newGateway(good(), srv.URL).Create(t.Context(), core.Payment{ID: 7, Amount: 30000})
	if err != nil || inv.ExternalID != testSession || inv.ExtAmount != 316 || inv.ExtCurrency != "USD" || inv.URL == "" {
		t.Fatalf("счёт: %+v %v", inv, err)
	}
	noBack := New(good(), srv.URL, nil)
	if _, err := noBack.Create(t.Context(), core.Payment{ID: 7, Amount: 30000}); err == nil {
		t.Error("без адреса возврата счёт создан")
	}
	badCur := good()
	badCur[keyCurrency] = "доллар"
	if newGateway(badCur, srv.URL).Configured() {
		t.Error("кривая валюта прошла")
	}
	badRate := good()
	badRate[keyRate] = "0"
	if newGateway(badRate, srv.URL).Configured() {
		t.Error("нулевой курс прошёл")
	}
}

func TestStatus(t *testing.T) {
	srv := fakeAPI(t)
	g := newGateway(good(), srv.URL)
	ev, err := g.Status(t.Context(), core.Payment{ID: 7, ExternalID: testSession})
	if err != nil || ev.Status != core.PayPaid || ev.ExtAmount != 316 || ev.PaymentID != 7 {
		t.Fatalf("статус: %+v %v", ev, err)
	}
	if _, err := g.Status(t.Context(), core.Payment{ExternalID: "cs_test_другая"}); err == nil {
		t.Error("ждали ошибку на чужую сессию")
	}
	eur := good()
	eur[keyCurrency] = "eur"
	if _, err := newGateway(eur, srv.URL).Status(t.Context(), core.Payment{ExternalID: testSession}); err == nil {
		t.Error("оплата в другой валюте принята")
	}
}

func TestEvents(t *testing.T) {
	g := newGateway(good(), "")
	p := core.Payment{ID: 7, ExternalID: testSession}
	session := func(typ, status string, amount int64) core.GatewayEvent {
		r := hook(t, "event_session.tmpl", p, amount, testHook, "{{TYPE}}", typ, "{{STATUS}}", status)
		ev, err := g.Verify(r)
		if err != nil {
			t.Fatalf("%s: %v", typ, err)
		}
		return ev
	}
	for name, tc := range map[string]struct {
		typ, status string
		want        core.PayStatus
		amount      int64
	}{
		"оплачено":          {"checkout.session.completed", "paid", core.PayPaid, 316},
		"ждём оплату":       {"checkout.session.completed", "unpaid", core.PayPending, 0},
		"отложенная оплата": {"checkout.session.async_payment_succeeded", "paid", core.PayPaid, 316},
		"отложенный отказ":  {"checkout.session.async_payment_failed", "unpaid", core.PayFailed, 0},
		"сессия истекла":    {"checkout.session.expired", "unpaid", core.PayFailed, 0},
		"чужое событие":     {"customer.created", "paid", core.PayPending, 0},
	} {
		ev := session(tc.typ, tc.status, 316)
		if ev.Status != tc.want || ev.ExtAmount != tc.amount {
			t.Errorf("%s: %+v", name, ev)
		}
		if tc.typ != "customer.created" && (ev.PaymentID != 7 || ev.ExternalID != testSession) {
			t.Errorf("%s: номера %+v", name, ev)
		}
	}
	refund := func(refunded, full string) (core.GatewayEvent, error) {
		return g.Verify(hook(t, "event_refund.tmpl", p, 0, testHook, "{{REFUNDED}}", refunded, "{{FULL}}", full))
	}
	if ev, err := refund("316", "true"); err != nil || ev.Status != core.PayRefunded || ev.PaymentID != 7 || ev.ExternalID != "" {
		t.Errorf("полный возврат: %+v %v", ev, err)
	}
	if ev, err := refund("100", "false"); err != nil || ev.Status != core.PayPending {
		t.Errorf("частичный возврат: %+v %v", ev, err)
	}
}

func TestSignature(t *testing.T) {
	body := []byte(`{"id":"evt_1"}`)
	now := time.Unix(1700000000, 0)
	good1 := hexOf(sign("1700000000", body, testHook))
	bad := strings.Repeat("0", 64)
	for name, tc := range map[string]struct {
		header string
		now    time.Time
		want   bool
	}{
		"одна подпись":               {"t=1700000000,v1=" + good1, now, true},
		"две подписи, верная вторая": {"t=1700000000,v1=" + bad + ",v1=" + good1, now, true},
		"две подписи, верная первая": {"t=1700000000,v1=" + good1 + ",v1=" + bad, now, true},
		"с v0 и пробелами":           {"t=1700000000, v0=" + bad + ", v1=" + good1, now, true},
		"все неверные":               {"t=1700000000,v1=" + bad + ",v1=" + bad, now, false},
		"старая метка":               {"t=1700000000,v1=" + good1, now.Add(6 * time.Minute), false},
		"метка из будущего":          {"t=1700000000,v1=" + good1, now.Add(-6 * time.Minute), false},
		"край окна":                  {"t=1700000000,v1=" + good1, now.Add(5 * time.Minute), true},
		"подменена метка":            {"t=1700000001,v1=" + good1, now, false},
		"нет метки":                  {"v1=" + good1, now, false},
		"нет v1":                     {"t=1700000000", now, false},
		"не hex":                     {"t=1700000000,v1=zz", now, false},
		"пусто":                      {"", now, false},
	} {
		if got := validSignature(body, tc.header, testHook, tc.now); got != tc.want {
			t.Errorf("%s: %v", name, got)
		}
	}
	if validSignature(body, "t=1700000000,v1="+good1, "", now) {
		t.Error("пустой секрет принят")
	}
	if validSignature([]byte(`{"id":"evt_2"}`), "t=1700000000,v1="+good1, testHook, now) {
		t.Error("подпись принята для другого тела")
	}
}

func TestKeyNotInError(t *testing.T) {
	srv := fakeAPI(t)
	bad := good()
	bad[keySecret] = "sk_test_неверный"
	for name, run := range map[string]func() error{
		"create": func() error {
			_, e := newGateway(bad, srv.URL).Create(t.Context(), core.Payment{ID: 1, Amount: 30000})
			return e
		},
		"status": func() error {
			_, e := newGateway(bad, srv.URL).Status(t.Context(), core.Payment{ExternalID: testSession})
			return e
		},
		"net": func() error {
			_, e := newGateway(bad, "http://127.0.0.1:1").Status(t.Context(), core.Payment{ExternalID: testSession})
			return e
		},
	} {
		if err := run(); err == nil || strings.Contains(err.Error(), "sk_test") || strings.Contains(err.Error(), "неверный") {
			t.Errorf("%s: ошибка %v", name, err)
		}
	}
}

func TestMinorFor(t *testing.T) {
	for _, tc := range []struct{ kop, rate, want int64 }{
		{30000, 9500, 316}, {9500, 9500, 100}, {9501, 9500, 101}, {1, 9500, 1}, {19000, 9500, 200},
	} {
		if got, err := minorFor(tc.kop, tc.rate); err != nil || got != tc.want {
			t.Errorf("%d/%d: %d %v", tc.kop, tc.rate, got, err)
		}
	}
	for _, tc := range [][2]int64{{0, 9500}, {-1, 9500}, {100, 0}, {100, -5}, {1 << 41, 9500}} {
		if _, err := minorFor(tc[0], tc[1]); err == nil {
			t.Errorf("%v: ждали ошибку", tc)
		}
	}
}

func FuzzVerify(f *testing.F) {
	f.Add(`{"type":"checkout.session.completed"}`, "t=1,v1=00")
	g := newGateway(good(), "")
	f.Fuzz(func(t *testing.T, body, sig string) {
		r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
		r.Header.Set("Stripe-Signature", sig)
		if _, err := g.Verify(r); err == nil {
			t.Skip("подпись угадана случайно, это не падение")
		}
	})
}

func TestCurrencyNeedsCents(t *testing.T) {
	for code, ok := range map[string]bool{"usd": true, "EUR": true, "jpy": false, "krw": false, "kwd": false, "us": false} {
		if _, got := currencyCode(code); got != ok {
			t.Fatalf("%s: %v, ждали %v", code, got, ok)
		}
	}
}
