package wata

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha512"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"github.com/bini69-oi/worker/pay"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bini69-oi/worker/core"
	"github.com/bini69-oi/worker/pay/gatewaytest"
)

type cfg map[string]string

func (c cfg) String(k string) string { return c[k] }
func (c cfg) Secret(k string) string { return c[k] }

// sample берёт записанный пример из testdata и подставляет номер платежа и сумму.
func sample(t *testing.T, name string, order int64, amount int64) string {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return strings.NewReplacer("{{ORDER}}", strconv.FormatInt(order, 10), "{{AMOUNT}}", pay.Rubles(amount)).Replace(string(b))
}

func newKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	k, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func pemOf(t *testing.T, k *rsa.PrivateKey) string {
	t.Helper()
	der, err := x509.MarshalPKIXPublicKey(&k.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))
}

func sign(t *testing.T, k *rsa.PrivateKey, body string) string {
	t.Helper()
	sum := sha512.Sum512([]byte(body))
	sig, err := rsa.SignPKCS1v15(rand.Reader, k, crypto.SHA512, sum[:])
	if err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(sig)
}

func hookReq(body, sig string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/pay/wata", strings.NewReader(body))
	r.Header.Set("X-Signature", sig)
	return r
}

// fakeAPI изображает API WATA. Ключ можно подменить на лету: так проверяется смена ключа.
type fakeAPI struct {
	t        *testing.T
	key      atomic.Pointer[rsa.PrivateKey]
	keyCalls atomic.Int32
	paid     atomic.Int64 // номер платежа, который WATA считает оплаченным
	amount   atomic.Int64
}

func (f *fakeAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") != "Bearer tok3n" {
		http.Error(w, "no", http.StatusUnauthorized)
		return
	}
	switch {
	case r.Method == http.MethodPost && r.URL.Path == "/links":
		var in struct {
			Amount  json.Number `json:"amount"`
			OrderID string      `json:"orderId"`
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		n, _ := strconv.ParseInt(in.OrderID, 10, 64)
		f.amount.Store(func() int64 { k, _ := pay.Kopecks(string(in.Amount)); return k }())
		_, _ = io.WriteString(w, sample(f.t, "links_created.json", n, 0))
	case r.URL.Path == "/public-key":
		f.keyCalls.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]string{"value": pemOf(f.t, f.key.Load())})
	case r.URL.Path == "/transactions":
		n, _ := strconv.ParseInt(r.URL.Query().Get("orderId"), 10, 64)
		if n == f.paid.Load() {
			_, _ = io.WriteString(w, sample(f.t, "transactions_paid.json", n, f.amount.Load()))
			return
		}
		_, _ = io.WriteString(w, `{"items":[],"totalCount":0}`)
	default:
		http.NotFound(w, r)
	}
}

func TestSuite(t *testing.T) {
	api := &fakeAPI{t: t}
	key, other := newKey(t), newKey(t)
	api.key.Store(key)
	srv := httptest.NewServer(api)
	defer srv.Close()
	g := New(cfg{"wata.token": "tok3n"}, srv.URL, srv.Client())
	mk := func(k *rsa.PrivateKey) func(core.Payment, int64) *http.Request {
		return func(p core.Payment, a int64) *http.Request {
			api.paid.Store(p.ID) // касса знает об оплате: вебхук подтверждается API
			body := sample(t, "webhook_paid.json", p.ID, a)
			return hookReq(body, sign(t, k, body))
		}
	}
	gatewaytest.Run(t, gatewaytest.Case{
		Gateway:      g,
		Unconfigured: New(cfg{}, srv.URL, srv.Client()),
		Paid:         mk(key),
		Forged:       mk(other),
		Amount:       30000,
	})
}

func TestVerifyTable(t *testing.T) {
	api := &fakeAPI{t: t}
	key := newKey(t)
	api.key.Store(key)
	srv := httptest.NewServer(api)
	defer srv.Close()
	g := New(cfg{"wata.token": "tok3n"}, srv.URL, srv.Client())
	paid, declined := sample(t, "webhook_paid.json", 7, 30050), sample(t, "webhook_declined.json", 7, 0)
	api.paid.Store(7)
	api.amount.Store(30050)
	usd := strings.Replace(paid, `"RUB"`, `"USD"`, 1)
	noOrder := strings.Replace(paid, `"orderId":"7"`, `"orderId":""`, 1)
	for _, c := range []struct {
		name, body, sig string
		status          core.PayStatus
		amount          int64
		bad             bool
	}{
		{"оплата", paid, sign(t, key, paid), core.PayPaid, 30050, false},
		{"отказ", declined, sign(t, key, declined), core.PayFailed, 0, false},
		{"без подписи", paid, "", "", 0, true},
		{"подпись не base64", paid, "%%%", "", 0, true},
		{"подпись от другого тела", paid, sign(t, key, declined), "", 0, true},
		{"доллары", usd, sign(t, key, usd), "", 0, true},
		{"нет номера платежа", noOrder, sign(t, key, noOrder), "", 0, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			ev, err := g.Verify(hookReq(c.body, c.sig))
			if c.bad {
				if err == nil {
					t.Fatalf("принят: %+v", ev)
				}
				return
			}
			if err != nil || ev.Status != c.status || ev.ExtAmount != c.amount || ev.PaymentID != 7 {
				t.Fatalf("%+v %v", ev, err)
			}
		})
	}
}

func TestStatusMap(t *testing.T) {
	for in, want := range map[string]core.PayStatus{
		"Paid": core.PayPaid, "paid": core.PayPaid, "Declined": core.PayFailed,
		"Pending": core.PayPending, "": core.PayPending, "Unknown": core.PayPending,
	} {
		if got := mapStatus(in); got != want {
			t.Errorf("%q: %s, ждали %s", in, got, want)
		}
	}
}

func TestStatusPolling(t *testing.T) {
	api := &fakeAPI{t: t}
	api.key.Store(newKey(t))
	api.paid.Store(5)
	api.amount.Store(15050)
	srv := httptest.NewServer(api)
	defer srv.Close()
	g := New(cfg{"wata.token": "tok3n"}, srv.URL, srv.Client())
	ev, err := g.Status(t.Context(), core.Payment{ID: 5, ExternalID: "link-5"})
	if err != nil || ev.Status != core.PayPaid || ev.ExtAmount != 15050 || ev.ExternalID != "link-5" {
		t.Fatalf("оплата: %+v %v", ev, err)
	}
	ev, err = g.Status(t.Context(), core.Payment{ID: 6, ExternalID: "link-6"})
	if err != nil || ev.Status != core.PayPending {
		t.Fatalf("ожидание: %+v %v", ev, err)
	}
}

// Ключ кэшируется, после срока и после смены ключа берётся заново, но не чаще раза в минуту.
func TestKeyCache(t *testing.T) {
	api := &fakeAPI{t: t}
	k1, k2 := newKey(t), newKey(t)
	api.key.Store(k1)
	srv := httptest.NewServer(api)
	defer srv.Close()
	g := New(cfg{"wata.token": "tok3n"}, srv.URL, srv.Client())
	now := time.Now()
	g.now = func() time.Time { return now }
	body := sample(t, "webhook_paid.json", 1, 100)
	api.paid.Store(1)
	api.amount.Store(100)
	for range 3 {
		if _, err := g.Verify(hookReq(body, sign(t, k1, body))); err != nil {
			t.Fatal(err)
		}
	}
	if api.keyCalls.Load() != 1 {
		t.Fatalf("ключ грузили %d раз", api.keyCalls.Load())
	}
	api.key.Store(k2)
	if _, err := g.Verify(hookReq(body, sign(t, k2, body))); err == nil {
		t.Fatal("новый ключ принят раньше, чем прошла минута")
	}
	now = now.Add(2 * time.Minute)
	if _, err := g.Verify(hookReq(body, sign(t, k2, body))); err != nil {
		t.Fatalf("после смены ключа: %v", err)
	}
	now = now.Add(2 * keyTTL)
	_, _ = g.Verify(hookReq(body, sign(t, k2, body)))
	if api.keyCalls.Load() != 3 {
		t.Fatalf("ключ грузили %d раз, ждали 3", api.keyCalls.Load())
	}
}

func TestSecretNotInError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "tok3n", 500) }))
	defer srv.Close()
	g := New(cfg{"wata.token": "tok3n"}, srv.URL, srv.Client())
	_, err := g.Status(t.Context(), core.Payment{ID: 1, ExternalID: "x"})
	if err == nil || strings.Contains(err.Error(), "tok3n") {
		t.Fatalf("ошибка: %v", err)
	}
	if _, err := g.Verify(hookReq("{}", "AAAA")); err == nil || strings.Contains(err.Error(), "tok3n") {
		t.Fatalf("ошибка проверки: %v", err)
	}
}

func TestParseKeyAndMoney(t *testing.T) {
	k := newKey(t)
	der, _ := x509.MarshalPKIXPublicKey(&k.PublicKey)
	for name, in := range map[string]string{
		"pkix pem":  pemOf(t, k),
		"pkcs1":     string(pem.EncodeToMemory(&pem.Block{Type: "RSA PUBLIC KEY", Bytes: x509.MarshalPKCS1PublicKey(&k.PublicKey)})),
		"голый b64": base64.StdEncoding.EncodeToString(der),
	} {
		if _, err := parseKey(in); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := parseKey("мусор"); err == nil {
		t.Error("мусор принят за ключ")
	}
	for in, want := range map[string]int64{"300": 30000, "300.5": 30050, "0.01": 1, "12.3400": 1234} {
		if got, err := pay.Kopecks(in); err != nil || got != want {
			t.Errorf("%s: %d %v", in, got, err)
		}
	}
	if _, err := pay.Kopecks("1.005"); err == nil {
		t.Error("доли копейки приняты")
	}
	if pay.Rubles(12305) != "123.05" {
		t.Error("rubles")
	}
}

// Подпись WATA общая для всех магазинов: подписанный вебхук об оплате, о которой наш API не знает,
// не зачисляется. Так чужой магазин WATA не перешлёт нам свой вебхук с нашим номером платежа.
func TestForeignMerchantHook(t *testing.T) {
	api := &fakeAPI{t: t}
	key := newKey(t)
	api.key.Store(key)
	srv := httptest.NewServer(api)
	defer srv.Close()
	g := New(cfg{"wata.token": "tok3n"}, srv.URL, srv.Client())
	body := sample(t, "webhook_paid.json", 9, 30000) // наш API про платёж 9 ничего не знает
	if ev, err := g.Verify(hookReq(body, sign(t, key, body))); err == nil {
		t.Fatalf("вебхук без оплаты у нашего магазина принят: %+v", ev)
	}
}
