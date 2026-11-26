package heleket

import (
	"encoding/json"
	"github.com/bini69-oi/worker/pay"
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
	testMerchant = "8b03432e-385b-4670-8d06-064591096795"
	testKey      = "test-key"
	testUUID     = "62f88b36-a9d5-4fa6-aa26-e040c3dbf26d"
)

type cfg map[string]string

func (c cfg) String(k string) string { return c[k] }
func (c cfg) Secret(k string) string { return c[k] }
func (c cfg) Int(k string) int64 {
	var n int64
	for _, r := range c[k] {
		n = n*10 + int64(r-'0')
	}
	return n
}

func good() cfg {
	return cfg{keyMerchant: testMerchant, keyAPIKey: testKey, keyCoin: " usdt ", keyNetwork: "tron",
		keyLifetime: "1800", keyCallback: "https://bot.example/pay/heleket"}
}

func data(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// fakeAPI изображает API Heleket: проверяет мерчанта и подпись запроса, отдаёт записанные ответы.
func fakeAPI(t *testing.T) *httptest.Server {
	t.Helper()
	create, info, denied := data(t, "create_ok.json"), data(t, "info_paid.json"), data(t, "error_401.json")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if r.Header.Get("merchant") != testMerchant || r.Header.Get("sign") != sign(body, testKey) {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write(denied)
			return
		}
		var req map[string]any
		_ = json.Unmarshal(body, &req)
		switch r.URL.Path {
		case "/payment":
			if req["currency"] != "RUB" || req["to_currency"] != "USDT" || req["network"] != "tron" ||
				req["lifetime"] != float64(1800) || req["url_callback"] != "https://bot.example/pay/heleket" ||
				!strings.HasPrefix(req["order_id"].(string), payloadPrefix) {
				t.Errorf("счёт создан не так: %v", req)
			}
			if strings.Contains(string(body), "/") && !strings.Contains(string(body), `\/`) {
				t.Errorf("слэши не экранированы: %s", body)
			}
			_, _ = w.Write(create)
		case "/payment/info":
			if req["uuid"] != testUUID {
				t.Errorf("статус спрошен не про наш счёт: %v", req)
			}
			_, _ = w.Write(info)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// hook собирает подписанный вебхук из шаблона в testdata.
func hook(p core.Payment, amount int64, key, status string) *http.Request {
	body := strings.NewReplacer("{{UUID}}", p.ExternalID, "{{ORDER}}", payloadPrefix+strconv.FormatInt(p.ID, 10), "{{AMOUNT}}", pay.Rubles(amount),
		"{{STATUS}}", status).Replace(string(mustRead("testdata/webhook.tmpl")))
	signed := body + `,"sign":"` + sign([]byte(body+"}"), key) + `"}`
	return httptest.NewRequest(http.MethodPost, "/pay/heleket", strings.NewReader(signed))
}

func mustRead(p string) []byte {
	b, err := os.ReadFile(p)
	if err != nil {
		panic(err)
	}
	return b
}

func TestGateway(t *testing.T) {
	srv := fakeAPI(t)
	gatewaytest.Run(t, gatewaytest.Case{
		Gateway:      New(good(), srv.URL, srv.Client()),
		Unconfigured: New(cfg{}, srv.URL, srv.Client()),
		Paid:         func(p core.Payment, a int64) *http.Request { return hook(p, a, testKey, "paid") },
		Forged:       func(p core.Payment, a int64) *http.Request { return hook(p, a, "чужой", "paid") },
		Amount:       30000,
	})
}

func TestStatus(t *testing.T) {
	srv := fakeAPI(t)
	g := New(good(), srv.URL, nil)
	ev, err := g.Status(t.Context(), core.Payment{ID: 7, ExternalID: testUUID})
	if err != nil || ev.Status != core.PayPaid || ev.ExtAmount != 30000 || ev.PaymentID != 7 {
		t.Fatalf("статус: %+v %v", ev, err)
	}
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(data(t, "info_paid.json"))
	}))
	defer other.Close()
	if _, err := New(good(), other.URL, nil).Status(t.Context(), core.Payment{ExternalID: "другой"}); err == nil {
		t.Fatal("ждали отказ на чужой счёт")
	}
}

// Записанный вебхук с подписью, посчитанной независимо (python), и его порча.
func TestRecordedWebhook(t *testing.T) {
	g := New(good(), "", nil)
	raw := string(data(t, "webhook_paid.json"))
	ev, err := g.Verify(httptest.NewRequest(http.MethodPost, "/pay/heleket", strings.NewReader(raw)))
	if err != nil || ev.Status != core.PayPaid || ev.ExtAmount != 30000 || ev.PaymentID != 7 || ev.ExternalID != testUUID {
		t.Fatalf("вебхук: %+v %v", ev, err)
	}
	// Слэши в теле без экранирования: подпись по той же записи PHP, должна сойтись.
	plain := strings.ReplaceAll(raw, `\/`, "/")
	if _, err := g.Verify(httptest.NewRequest(http.MethodPost, "/", strings.NewReader(plain))); err != nil {
		t.Fatalf("вебхук без экранирования слэшей: %v", err)
	}
	for name, bad := range map[string]string{
		"сумма":      strings.Replace(raw, `"amount":"300.00"`, `"amount":"1.00"`, 1),
		"без sign":   strings.Replace(raw, `,"sign"`, `,"x"`, 1),
		"пустое":     "",
		"не json":    "sign",
		"верхний":    strings.ToUpper(raw),
		"подпись 00": raw[:len(raw)-3] + `00"}`,
	} {
		if _, err := g.Verify(httptest.NewRequest(http.MethodPost, "/", strings.NewReader(bad))); err == nil {
			t.Errorf("%s: принят", name)
		}
	}
	if _, err := New(cfg{}, "", nil).Verify(httptest.NewRequest(http.MethodPost, "/", strings.NewReader(raw))); err == nil {
		t.Error("без ключа вебхук принят")
	}
}

func TestMapStatus(t *testing.T) {
	for in, want := range map[string]core.PayStatus{
		"paid": core.PayPaid, "paid_over": core.PayPaid, "PAID": core.PayPaid,
		"cancel": core.PayFailed, "fail": core.PayFailed, "system_fail": core.PayFailed,
		"refund_paid": core.PayRefunded,
		"check":       core.PayPending, "process": core.PayPending, "confirm_check": core.PayPending,
		"wrong_amount": core.PayPending, "locked": core.PayPending, "refund_process": core.PayPending,
		"refund_fail": core.PayPending, "": core.PayPending, "новый": core.PayPending,
	} {
		if got := mapStatus(in); got != want {
			t.Errorf("%q: %s, ждали %s", in, got, want)
		}
	}
}

func TestOnlyPaidCarriesAmount(t *testing.T) {
	for _, st := range []string{"check", "cancel", "refund_paid", "wrong_amount"} {
		ev, err := toEvent(payment{UUID: "u", Status: st, Amount: "300.00", Currency: "RUB"})
		if err != nil || ev.ExtAmount != 0 {
			t.Errorf("%s: %+v %v", st, ev, err)
		}
	}
	for _, p := range []payment{
		{UUID: "u", Status: "paid", Amount: "300.00", Currency: "USD"},
		{UUID: "u", Status: "paid", Amount: "300.00"},
		{UUID: "u", Status: "paid", Currency: "RUB"},
		{UUID: "u", Status: "paid", Amount: "3.005", Currency: "RUB"},
	} {
		if _, err := toEvent(p); err == nil {
			t.Errorf("оплата принята: %+v", p)
		}
	}
}

func TestKeyNotInError(t *testing.T) {
	srv := fakeAPI(t)
	bad := cfg{keyMerchant: testMerchant, keyAPIKey: "секретный-ключ"}
	for name, run := range map[string]func() error{
		"create": func() error {
			_, e := New(bad, srv.URL, nil).Create(t.Context(), core.Payment{ID: 1, Amount: 100})
			return e
		},
		"status": func() error {
			_, e := New(bad, srv.URL, nil).Status(t.Context(), core.Payment{ExternalID: "x"})
			return e
		},
		"net": func() error {
			_, e := New(bad, "http://127.0.0.1:1", nil).Status(t.Context(), core.Payment{})
			return e
		},
	} {
		if err := run(); err == nil || strings.Contains(err.Error(), "секретный-ключ") || strings.Contains(err.Error(), testMerchant) {
			t.Errorf("%s: ошибка %v", name, err)
		}
	}
}

func TestMoney(t *testing.T) {
	for in, want := range map[string]int64{"300": 30000, "300.5": 30050, "300.00": 30000, "0.01": 1, "12.3400": 1234} {
		if got, err := pay.Kopecks(in); err != nil || got != want {
			t.Errorf("%s: %d %v", in, got, err)
		}
	}
	for _, in := range []string{"", "abc", "-5", "1.005", "1e3", ".", "1,5", "1.2.3"} {
		if _, err := pay.Kopecks(in); err == nil {
			t.Errorf("%q: ждали ошибку", in)
		}
	}
	if pay.Rubles(12305) != "123.05" || pay.Rubles(30000) != "300.00" {
		t.Error("rubles")
	}
}

func TestLifetime(t *testing.T) {
	for in, want := range map[int64]int64{0: 3600, 299: 3600, 300: 300, 43200: 43200, 43201: 3600} {
		if got := lifetime(in); got != want {
			t.Errorf("%d: %d", in, got)
		}
	}
}

func FuzzVerify(f *testing.F) {
	f.Add(`{"uuid":"u","sign":"00"}`)
	f.Add(`{"sign":"ab","uuid":"u"}`)
	g := New(good(), "", nil)
	f.Fuzz(func(t *testing.T, body string) {
		if _, err := g.Verify(httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))); err == nil {
			t.Skip("подпись угадана случайно, это невозможно, но не падение")
		}
	})
}
