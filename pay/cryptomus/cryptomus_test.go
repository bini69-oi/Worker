package cryptomus

import (
	"encoding/json"
	"fmt"
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

const (
	testMerchant = "8b03432e-385b-4c70-b446-1ae1a1d0d9e5"
	testKey      = "test-payment-key"
	fixtureUUID  = "6f8a4c1e-1b2d-4e7a-9c3b-5d0e8f7a1b2c"
)

type cfg map[string]string

func (c cfg) String(k string) string { return c[k] }
func (c cfg) Secret(k string) string { return c[k] }

var good = cfg{keyMerchant: testMerchant, keyAPIKey: testKey, keyCallback: "https://bot.example/pay/cryptomus"}

func fixture(t testing.TB, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// fakeAPI изображает API Cryptomus: проверяет заголовки и подпись запроса, отвечает записанными примерами.
func fakeAPI(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if r.Header.Get("merchant") != testMerchant || r.Header.Get("sign") != signature(body, testKey) {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write(fixture(t, "error.json"))
			return
		}
		var req map[string]any
		_ = json.Unmarshal(body, &req)
		switch strings.TrimPrefix(r.URL.Path, "/") {
		case "payment":
			if req["currency"] != "RUB" || req["url_callback"] != "https://bot.example/pay/cryptomus" ||
				!strings.HasPrefix(fmt.Sprint(req["order_id"]), orderPrefix) {
				t.Errorf("счёт создан не так: %v", req)
			}
			if _, err := pay.Kopecks(fmt.Sprint(req["amount"])); err != nil {
				t.Errorf("сумма счёта: %v", err)
			}
			_, _ = w.Write(fixture(t, "create_ok.json"))
		case "payment/info":
			if req["uuid"] == nil {
				t.Errorf("статус спрошен не по uuid: %v", req)
			}
			_, _ = w.Write(fixture(t, "info_paid.json"))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// hook собирает вебхук так, как его шлёт Cryptomus (PHP): слэши экранированы, sign внутри тела.
func hook(key string, p core.Payment, extAmount int64) *http.Request {
	body := fmt.Sprintf(`{"type":"payment","uuid":%q,"order_id":"worker_%d","amount":%q,"is_final":true,`+
		`"status":"paid","currency":"RUB","additional_data":"https:\/\/t.me\/bot?start=paid_%d"}`,
		p.ExternalID, p.ID, formatKopeks(extAmount), p.ID)
	signed := body[:len(body)-1] + `,"sign":"` + signature([]byte(body), key) + `"}`
	return httptest.NewRequest(http.MethodPost, "/pay/cryptomus", strings.NewReader(signed))
}

func TestGateway(t *testing.T) {
	srv := fakeAPI(t)
	gatewaytest.Run(t, gatewaytest.Case{
		Gateway:      New(good, srv.URL, srv.Client()),
		Unconfigured: New(cfg{keyMerchant: testMerchant}, srv.URL, srv.Client()),
		Paid:         func(p core.Payment, a int64) *http.Request { return hook(testKey, p, a) },
		Forged:       func(p core.Payment, a int64) *http.Request { return hook("чужой-ключ", p, a) },
		Amount:       30000,
	})
}

func TestCreateAndStatus(t *testing.T) {
	srv := fakeAPI(t)
	g := New(good, srv.URL, nil)
	g.Return = func(id int64) string { return "https://t.me/bot?start=paid_" + strconv.FormatInt(id, 10) }
	inv, err := g.Create(t.Context(), core.Payment{ID: 7, Amount: 30000})
	if err != nil || inv.ExtAmount != 30000 || inv.ExtCurrency != "RUB" || inv.ExternalID != fixtureUUID || inv.URL == "" {
		t.Fatalf("счёт: %+v %v", inv, err)
	}
	ev, err := g.Status(t.Context(), core.Payment{ID: 7, ExternalID: fixtureUUID})
	if err != nil || ev.Status != core.PayPaid || ev.ExtAmount != 30000 || ev.PaymentID != 7 {
		t.Fatalf("статус: %+v %v", ev, err)
	}
	if _, err := g.Status(t.Context(), core.Payment{ID: 7, ExternalID: "другой"}); err == nil {
		t.Fatal("ответ про чужой счёт принят")
	}
}

func TestErrorsHideKeys(t *testing.T) {
	srv := fakeAPI(t)
	bad := New(cfg{keyMerchant: testMerchant, keyAPIKey: "секретный-чужой"}, srv.URL, nil)
	_, err := bad.Create(t.Context(), core.Payment{ID: 1, Amount: 100})
	if err == nil || strings.Contains(err.Error(), "секретный-чужой") || strings.Contains(err.Error(), testMerchant) {
		t.Fatalf("ждали отказ без ключей в тексте: %v", err)
	}
	if _, err := New(cfg{}, srv.URL, nil).Create(t.Context(), core.Payment{ID: 1, Amount: 100}); err == nil {
		t.Fatal("касса без ключей создала счёт")
	}
}

// Таблица статусов: вебхуки из testdata (подписаны ключом testKey по схеме Cryptomus).
func TestVerifyStatuses(t *testing.T) {
	g := New(good, "", nil)
	cases := []struct {
		file   string
		status core.PayStatus
		amount int64
		err    bool
	}{
		{"webhook_paid.json", core.PayPaid, 30000, false},
		{"webhook_paid_over.json", core.PayPaid, 30000, false}, // переплата: зачисляем сумму счёта, не больше
		{"webhook_wrong_amount.json", core.PayPending, 0, false},
		{"webhook_check.json", core.PayPending, 0, false},
		{"webhook_cancel.json", core.PayExpired, 0, false},
		{"webhook_fail.json", core.PayFailed, 0, false},
		{"webhook_refund_paid.json", core.PayRefunded, 0, false},
		{"webhook_usd.json", "", 0, true}, // оплаченный счёт не в рублях не принимаем
	}
	for _, c := range cases {
		r := httptest.NewRequest(http.MethodPost, "/pay/cryptomus", strings.NewReader(string(fixture(t, c.file))))
		ev, err := g.Verify(r)
		if c.err != (err != nil) {
			t.Errorf("%s: ошибка %v, ждали ошибку=%v", c.file, err, c.err)
			continue
		}
		if !c.err && (ev.Status != c.status || ev.ExtAmount != c.amount || ev.PaymentID != 7 || ev.ExternalID != fixtureUUID) {
			t.Errorf("%s: %+v", c.file, ev)
		}
	}
}

// Слэш в теле: Cryptomus подписывает "\/", а JSON с "/" или с пробелами должен проверяться так же.
func TestVerifySlashAndSpacing(t *testing.T) {
	g := New(good, "", nil)
	raw := string(fixture(t, "webhook_paid.json"))
	if !strings.Contains(raw, `\/`) {
		t.Fatal("в образце нет экранированного слэша")
	}
	plain := strings.ReplaceAll(raw, `\/`, "/")
	pretty := strings.ReplaceAll(strings.ReplaceAll(raw, `,"`, ",\n  \""), `:`, ": ")
	pretty = strings.ReplaceAll(pretty, `https: \/\/`, `https:\/\/`) // двоеточие в адресе не трогаем
	for name, body := range map[string]string{"как у PHP": raw, "слэш без экранирования": plain, "с пробелами": pretty} {
		if _, err := g.Verify(httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	// Подпись без экранирования слэша (так считает json.dumps или JSON.stringify) не годится.
	unescaped := strings.ReplaceAll(strings.TrimSpace(raw), `\/`, "/")
	i := strings.Index(unescaped, `,"sign"`)
	body := unescaped[:i] + "}"
	forged := unescaped[:i] + `,"sign":"` + signature([]byte(body), testKey) + `"}`
	if _, err := g.Verify(httptest.NewRequest(http.MethodPost, "/", strings.NewReader(forged))); err != pay.ErrBadSignature {
		t.Errorf("подпись без экранирования слэша принята: %v", err)
	}
}

func TestVerifyRejects(t *testing.T) {
	g := New(good, "", nil)
	raw := strings.TrimSpace(string(fixture(t, "webhook_paid.json")))
	noSign := raw[:strings.Index(raw, `,"sign"`)] + "}"
	tampered := strings.Replace(raw, `"300.00"`, `"1.00"`, 1)
	for name, body := range map[string]string{
		"без sign":      noSign,
		"тело изменено": tampered,
		"sign не hex":   strings.Replace(raw, `"sign":"b`, `"sign":"z`, 1),
		"не json":       "<xml/>",
		"пустое":        "",
	} {
		_, err := g.Verify(httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body)))
		if err == nil {
			t.Errorf("%s: принято", name)
		}
	}
	// Ключ не задан: даже верная по форме подпись пустым ключом не проходит.
	if _, err := New(cfg{}, "", nil).Verify(httptest.NewRequest(http.MethodPost, "/", strings.NewReader(raw))); err == nil {
		t.Error("вебхук принят без ключа")
	}
}

func TestWalletEventIgnored(t *testing.T) {
	g := New(good, "", nil)
	body := `{"type":"wallet","uuid":"x","status":"paid"}`
	signed := body[:len(body)-1] + `,"sign":"` + signature([]byte(body), testKey) + `"}`
	ev, err := g.Verify(httptest.NewRequest(http.MethodPost, "/", strings.NewReader(signed)))
	if err != nil || ev.Status != core.PayPending {
		t.Fatalf("кошельковое событие: %+v %v", ev, err)
	}
}

func TestCanonical(t *testing.T) {
	for in, want := range map[string]string{
		`{"a": 1, "sign": "x"}`:                   `{"a":1}`,
		`{"sign":"x","a":1}`:                      `{"a":1}`,
		`{"a":1,"sign":"x","b":2}`:                `{"a":1,"b":2}`,
		`{"a":"x/y","n":{"sign":"keep"}}`:         `{"a":"x\/y","n":{"sign":"keep"}}`,
		`{"v":"sign","k":"a, \"b\" /","sign":""}`: `{"v":"sign","k":"a, \"b\" \/"}`,
		`{"a":"\/","sign":"q"}`:                   `{"a":"\/"}`,
	} {
		if got := string(canonical([]byte(in))); got != want {
			t.Errorf("%s: получили %s, ждали %s", in, got, want)
		}
	}
}

func FuzzVerify(f *testing.F) {
	for _, n := range []string{"webhook_paid.json", "webhook_paid_over.json", "webhook_cancel.json"} {
		f.Add(fixture(f, n))
	}
	f.Add([]byte(`{"sign":"`))
	f.Add([]byte(`{"a":{"sign":[1,{"x":"\`))
	g := New(good, "", nil)
	f.Fuzz(func(t *testing.T, body []byte) {
		// Ни паники, ни принятого вебхука без верной подписи.
		ev, err := g.Verify(httptest.NewRequest(http.MethodPost, "/", strings.NewReader(string(body))))
		if err == nil && ev.Status == core.PayPaid && !strings.Contains(string(body), `"sign"`) {
			t.Fatalf("принят вебхук без sign: %q", body)
		}
	})
}

func TestParseKopeks(t *testing.T) {
	for in, want := range map[string]int64{"300.00": 30000, "123.4": 12340, "5": 500, "0.07": 7, "10.500": 1050} {
		if got, err := pay.Kopecks(in); err != nil || got != want {
			t.Errorf("%q: %d %v, ждали %d", in, got, err, want)
		}
	}
	for _, in := range []string{"", "abc", "-5", "1.2.3", "1e3", "10.501", ".", "1,5"} {
		if _, err := pay.Kopecks(in); err == nil {
			t.Errorf("%q: ждали ошибку", in)
		}
	}
}

func TestAmountStringOrNumber(t *testing.T) {
	for raw, want := range map[string]string{`{"amount":"300.00"}`: "300.00", `{"amount":300}`: "300", `{"amount":123.4}`: "123.4"} {
		var p payment
		if err := json.Unmarshal([]byte(raw), &p); err != nil || string(p.Amount) != want {
			t.Errorf("%s: получили %q (%v)", raw, p.Amount, err)
		}
	}
}
