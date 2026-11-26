package antilopay

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
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

	"github.com/bini69-oi/worker/core"
	"github.com/bini69-oi/worker/pay/gatewaytest"
)

type cfg map[string]string

func (c cfg) String(k string) string { return c[k] }
func (c cfg) Secret(k string) string { return c[k] }

func newKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	k, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func privPEM(k *rsa.PrivateKey) string {
	der, _ := x509.MarshalPKCS8PrivateKey(k)
	return string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))
}

func pubPEM(k *rsa.PrivateKey) string {
	der, _ := x509.MarshalPKIXPublicKey(&k.PublicKey)
	return string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))
}

// sample берёт записанный пример из testdata и подставляет номер заказа и сумму.
func sample(t *testing.T, name string, order, amount int64) string {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return strings.NewReplacer("{{ORDER}}", strconv.FormatInt(order, 10), "{{AMOUNT}}", pay.Rubles(amount),
		"{{FEE_AMOUNT}}", pay.Rubles(amount/2)).Replace(string(b))
}

func hookReq(body, sig string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/pay/antilopay", strings.NewReader(body))
	r.Header.Set("X-Apay-Callback", sig)
	return r
}

// fakeAPI изображает API Antilopay и проверяет подпись запросов открытым ключом магазина.
type fakeAPI struct {
	t      *testing.T
	shop   *rsa.PrivateKey
	paid   atomic.Int64 // номер заказа, который касса считает оплаченным
	amount atomic.Int64
	last   atomic.Value // последнее тело create
}

func (f *fakeAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	if r.Header.Get("X-Apay-Secret-Id") != "sec-1" || r.Header.Get("X-Apay-Sign-Version") != "1" ||
		checkSignature(pubPEM(f.shop), body, r.Header.Get("X-Apay-Sign")) != nil {
		http.Error(w, `{"code":1}`, http.StatusUnauthorized)
		return
	}
	var in struct {
		Project string      `json:"project_identificator"`
		Order   string      `json:"order_id"`
		Amount  json.Number `json:"amount"`
	}
	if err := json.Unmarshal(body, &in); err != nil || in.Project != "proj-1" {
		http.Error(w, `{"code":2}`, http.StatusBadRequest)
		return
	}
	n, _ := strconv.ParseInt(in.Order, 10, 64)
	switch r.URL.Path {
	case "/payment/create":
		f.last.Store(string(body))
		k, _ := pay.Kopecks(string(in.Amount))
		f.amount.Store(k)
		_, _ = io.WriteString(w, sample(f.t, "create_ok.json", n, 0))
	case "/payment/check":
		if n == f.paid.Load() {
			_, _ = io.WriteString(w, sample(f.t, "check_paid.json", n, f.amount.Load()))
			return
		}
		_, _ = io.WriteString(w, sample(f.t, "check_pending.json", n, 0))
	default:
		http.NotFound(w, r)
	}
}

func setup(t *testing.T) (*Gateway, *fakeAPI, *rsa.PrivateKey, *httptest.Server) {
	t.Helper()
	shop, hook := newKey(t), newKey(t)
	api := &fakeAPI{t: t, shop: shop}
	srv := httptest.NewServer(api)
	t.Cleanup(srv.Close)
	c := cfg{keyProject: "proj-1", keySecret: "sec-1", keyPrivate: privPEM(shop), keyPublic: pubPEM(hook)}
	return New(c, srv.URL, srv.Client()), api, hook, srv
}

func TestSuite(t *testing.T) {
	g, _, hook, srv := setup(t)
	other := newKey(t)
	mk := func(k *rsa.PrivateKey) func(core.Payment, int64) *http.Request {
		return func(p core.Payment, a int64) *http.Request {
			body := sample(t, "webhook_paid.json", p.ID, a)
			sig, err := signBody(privPEM(k), []byte(body))
			if err != nil {
				t.Fatal(err)
			}
			return hookReq(body, sig)
		}
	}
	gatewaytest.Run(t, gatewaytest.Case{
		Gateway:      g,
		Unconfigured: New(cfg{}, srv.URL, srv.Client()),
		Paid:         mk(hook),
		Forged:       mk(other),
		Amount:       30000,
	})
}

func TestVerifyTable(t *testing.T) {
	g, _, hook, _ := setup(t)
	sig := func(body string) string {
		s, _ := signBody(privPEM(hook), []byte(body))
		return s
	}
	paid, fail := sample(t, "webhook_paid.json", 7, 30050), sample(t, "webhook_fail.json", 7, 0)
	usd := strings.Replace(paid, `"RUB"`, `"USD"`, 1)
	noOrder := strings.Replace(paid, `"order_id":"7"`, `"order_id":""`, 1)
	spaced := paid + "\n"
	for _, c := range []struct {
		name, body, sig string
		status          core.PayStatus
		amount          int64
		bad             bool
	}{
		{"оплата", paid, sig(paid), core.PayPaid, 30050, false},
		{"отказ", fail, sig(fail), core.PayFailed, 0, false},
		{"без подписи", paid, "", "", 0, true},
		{"подпись не base64", paid, "%%%", "", 0, true},
		{"подпись от другого тела", paid, sig(fail), "", 0, true},
		{"тело изменено на байт", spaced, sig(paid), "", 0, true},
		{"доллары", usd, sig(usd), "", 0, true},
		{"нет номера заказа", noOrder, sig(noOrder), "", 0, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			ev, err := g.Verify(hookReq(c.body, c.sig))
			if c.bad {
				if err == nil {
					t.Fatalf("принят: %+v", ev)
				}
				return
			}
			if err != nil || ev.Status != c.status || ev.ExtAmount != c.amount || ev.PaymentID != 7 || ev.ExternalID != "ap-7f3a91" {
				t.Fatalf("%+v %v", ev, err)
			}
		})
	}
}

func TestStatusMap(t *testing.T) {
	for in, want := range map[string]core.PayStatus{
		"SUCCESS": core.PayPaid, "success": core.PayPaid, "FAIL": core.PayFailed, "CANCEL": core.PayFailed,
		"EXPIRED": core.PayFailed, "CHARGEBACK": core.PayFailed, "REVERSED": core.PayFailed,
		"PENDING": core.PayPending, "": core.PayPending, "NEW": core.PayPending,
	} {
		if got := mapStatus(in); got != want {
			t.Errorf("%q: %s, ждали %s", in, got, want)
		}
	}
}

func TestStatusPolling(t *testing.T) {
	g, api, _, _ := setup(t)
	api.paid.Store(5)
	api.amount.Store(15050)
	ev, err := g.Status(t.Context(), core.Payment{ID: 5, ExternalID: "ap-5"})
	if err != nil || ev.Status != core.PayPaid || ev.ExtAmount != 15050 || ev.ExternalID != "ap-5" {
		t.Fatalf("оплата: %+v %v", ev, err)
	}
	ev, err = g.Status(t.Context(), core.Payment{ID: 6})
	if err != nil || ev.Status != core.PayPending {
		t.Fatalf("ожидание: %+v %v", ev, err)
	}
}

func TestCreateBody(t *testing.T) {
	g, api, _, _ := setup(t)
	g.cfg.(cfg)[keyMethod] = "sbp"
	g.Return = func(id int64) string { return "https://t.me/bot?start=paid_" + strconv.FormatInt(id, 10) }
	inv, err := g.Create(t.Context(), core.Payment{ID: 9, UserID: 3, Amount: 12345})
	if err != nil || inv.ExternalID != "ap-7f3a91" || inv.ExtAmount != 12345 || inv.ExtCurrency != "RUB" {
		t.Fatalf("%+v %v", inv, err)
	}
	body, _ := api.last.Load().(string)
	for _, want := range []string{`"amount":123.45`, `"order_id":"9"`, `"prefer_methods":["SBP"]`, `"success_url":"https://t.me/bot?start=paid_9"`} {
		if !strings.Contains(body, want) {
			t.Errorf("в запросе нет %s: %s", want, body)
		}
	}
}

func TestErrorsHideKeys(t *testing.T) {
	g, _, _, _ := setup(t)
	g.cfg.(cfg)[keyPrivate] = "SECRETKEYTEXT"
	_, err := g.Create(t.Context(), core.Payment{ID: 1, Amount: 100})
	if err == nil || strings.Contains(err.Error(), "SECRETKEYTEXT") {
		t.Fatalf("ждали ошибку без ключа: %v", err)
	}
	g.cfg.(cfg)[keyPrivate] = ""
	if g.Configured() {
		t.Fatal("без закрытого ключа касса настроена")
	}
}

func TestKopecks(t *testing.T) {
	for in, want := range map[string]int64{"300": 30000, "300.5": 30050, "300.50": 30050, "0.01": 1, "123.450": 12345} {
		if got, err := pay.Kopecks(in); err != nil || got != want {
			t.Errorf("%q: %d %v", in, got, err)
		}
	}
	for _, in := range []string{"", "1e5", "-1", "1.005", "abc"} {
		if _, err := pay.Kopecks(in); err == nil {
			t.Errorf("%q принято", in)
		}
	}
	if !bytes.Equal([]byte(pay.Rubles(5)), []byte("0.05")) {
		t.Error("rubles")
	}
}
