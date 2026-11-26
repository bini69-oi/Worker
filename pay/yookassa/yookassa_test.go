package yookassa

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/bini69-oi/worker/core"
	"github.com/bini69-oi/worker/pay"
	"github.com/bini69-oi/worker/pay/gatewaytest"
)

type cfg map[string]string

func (c cfg) String(k string) string { return c[k] }
func (c cfg) Secret(k string) string { return c[k] }

type remote struct {
	status   string
	value    string
	meta     string
	refunded string
}

// fakeAPI это поддельный API ЮKassa.
type fakeAPI struct {
	mu      sync.Mutex
	pays    map[string]remote
	created []map[string]any
	idem    []string
	authOK  bool
	nextID  int
	srv     *httptest.Server
}

func newFakeAPI(t *testing.T) *fakeAPI {
	f := &fakeAPI{pays: map[string]remote{}, authOK: true}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeAPI) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if u, p, ok := r.BasicAuth(); !ok || u != "shop1" || p != "sekret" {
		f.authOK = false
		http.Error(w, "{}", http.StatusUnauthorized)
		return
	}
	switch {
	case r.Method == http.MethodPost && r.URL.Path == "/payments":
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.created = append(f.created, body)
		f.idem = append(f.idem, r.Header.Get("Idempotence-Key"))
		f.nextID++
		id := fmt.Sprintf("yk-%d", f.nextID)
		amount := body["amount"].(map[string]any)["value"].(string)
		meta := body["metadata"].(map[string]any)["payment_id"].(string)
		f.pays[id] = remote{status: "pending", value: amount, meta: meta}
		writePay(w, id, f.pays[id])
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/payments/"):
		id := strings.TrimPrefix(r.URL.Path, "/payments/")
		p, ok := f.pays[id]
		if !ok {
			http.Error(w, "{}", http.StatusNotFound)
			return
		}
		writePay(w, id, p)
	default:
		http.NotFound(w, r)
	}
}

func writePay(w http.ResponseWriter, id string, p remote) {
	_ = json.NewEncoder(w).Encode(map[string]any{
		"id": id, "status": p.status,
		"amount":          map[string]string{"value": p.value, "currency": "RUB"},
		"confirmation":    map[string]string{"type": "redirect", "confirmation_url": "https://pay.test/" + id},
		"metadata":        map[string]string{"payment_id": p.meta},
		"refunded_amount": map[string]string{"value": p.refunded, "currency": "RUB"},
	})
}

func (f *fakeAPI) set(id string, p remote) {
	f.mu.Lock()
	f.pays[id] = p
	f.mu.Unlock()
}

func hook(event, id string) *http.Request {
	body := fmt.Sprintf(`{"type":"notification","event":%q,"object":{"id":%q}}`, event, id)
	return httptest.NewRequest(http.MethodPost, "/pay/yookassa", strings.NewReader(body))
}

func newGateway(f *fakeAPI) *Gateway {
	return New(cfg{"yookassa.shop_id": "shop1", "yookassa.secret_key": "sekret", "yookassa.return_url": "https://t.me/bot"}, f.srv.URL, nil)
}

func TestSuite(t *testing.T) {
	f := newFakeAPI(t)
	g := newGateway(f)
	gatewaytest.Run(t, gatewaytest.Case{
		Gateway:      g,
		Unconfigured: New(cfg{}, f.srv.URL, nil),
		Amount:       30000,
		Paid: func(p core.Payment, ext int64) *http.Request {
			f.set(p.ExternalID, remote{status: "succeeded", value: formatMoney(ext), meta: strconv.FormatInt(p.ID, 10)})
			return hook("payment.succeeded", p.ExternalID)
		},
		Forged: func(core.Payment, int64) *http.Request {
			return hook("payment.succeeded", "yk-unknown")
		},
	})
}

func TestMoneyFormat(t *testing.T) {
	cases := map[int64]string{30000: "300.00", 12345: "123.45", 5: "0.05", 100: "1.00"}
	for cents, want := range cases {
		if got := formatMoney(cents); got != want {
			t.Errorf("formatMoney(%d) = %q, ждали %q", cents, got, want)
		}
		if back, err := pay.Kopecks(want); err != nil || back != cents {
			t.Errorf("pay.Kopecks(%q) = %d %v", want, back, err)
		}
	}
	if _, err := pay.Kopecks("1.234"); err == nil {
		t.Error("три знака после точки должны давать ошибку")
	}
}

func TestCreateRequest(t *testing.T) {
	f := newFakeAPI(t)
	g := newGateway(f)
	for _, amount := range []int64{30000, 12345} {
		inv, err := g.Create(t.Context(), core.Payment{ID: 7, Amount: amount})
		if err != nil {
			t.Fatal(err)
		}
		if inv.ExtAmount != amount || inv.ExtCurrency != "RUB" || inv.URL == "" || inv.ExternalID == "" {
			t.Fatalf("счёт не тот: %+v", inv)
		}
	}
	want := []string{"300.00", "123.45"}
	for i, body := range f.created {
		if got := body["amount"].(map[string]any)["value"]; got != want[i] {
			t.Errorf("в API ушло %v, ждали %s", got, want[i])
		}
		if body["capture"] != true || body["confirmation"].(map[string]any)["return_url"] != "https://t.me/bot" {
			t.Errorf("тело запроса не то: %v", body)
		}
	}
	if f.idem[0] != "worker-7" {
		t.Errorf("Idempotence-Key = %q", f.idem[0])
	}
}

func TestStatus(t *testing.T) {
	f := newFakeAPI(t)
	g := newGateway(f)
	f.set("a", remote{status: "pending", value: "1.00", meta: "1"})
	f.set("b", remote{status: "succeeded", value: "1.00", meta: "1"})
	f.set("c", remote{status: "canceled", value: "1.00", meta: "1"})
	want := map[string]core.PayStatus{"a": core.PayPending, "b": core.PayPaid, "c": core.PayFailed}
	for id, st := range want {
		ev, err := g.Status(t.Context(), core.Payment{ExternalID: id})
		if err != nil || ev.Status != st || ev.ExtAmount != 100 {
			t.Errorf("%s: %+v %v", id, ev, err)
		}
	}
}

func TestVerifyTrustsOnlyAPI(t *testing.T) {
	f := newFakeAPI(t)
	g := newGateway(f)
	f.set("wait", remote{status: "pending", value: "1.00", meta: "1"})
	f.set("paid", remote{status: "succeeded", value: "5.00", meta: "9"})
	if _, err := g.Verify(hook("payment.succeeded", "wait")); err != pay.ErrBadSignature {
		t.Errorf("неоплаченный платёж: %v", err)
	}
	if _, err := g.Verify(hook("payment.succeeded", "nope")); err != pay.ErrBadSignature {
		t.Errorf("неизвестный платёж: %v", err)
	}
	if _, err := g.Verify(hook("payout.succeeded", "paid")); err != pay.ErrBadSignature {
		t.Errorf("чужое событие: %v", err)
	}
	if _, err := g.Verify(httptest.NewRequest(http.MethodPost, "/", io.NopCloser(strings.NewReader("не json")))); err != pay.ErrBadSignature {
		t.Errorf("мусор в теле: %v", err)
	}
	ev, err := g.Verify(hook("payment.succeeded", "paid"))
	if err != nil || ev.Status != core.PayPaid || ev.ExtAmount != 500 || ev.PaymentID != 9 {
		t.Errorf("оплаченный платёж: %+v %v", ev, err)
	}
}

func TestIPRestriction(t *testing.T) {
	f := newFakeAPI(t)
	g := newGateway(f)
	f.set("paid", remote{status: "succeeded", value: "1.00", meta: "1"})
	g.RestrictIPs(OfficialNets())
	r := hook("payment.succeeded", "paid")
	r.RemoteAddr = "203.0.113.5:4000"
	if _, err := g.Verify(r); err != pay.ErrBadSignature {
		t.Errorf("чужой адрес прошёл: %v", err)
	}
	r = hook("payment.succeeded", "paid")
	r.RemoteAddr = net.JoinHostPort("185.71.76.10", "4000")
	if _, err := g.Verify(r); err != nil {
		t.Errorf("адрес ЮKassa не прошёл: %v", err)
	}
}

func TestSecretNotInError(t *testing.T) {
	f := newFakeAPI(t)
	g := New(cfg{"yookassa.shop_id": "shop1", "yookassa.secret_key": "wrong-secret"}, f.srv.URL, nil)
	_, err := g.Create(t.Context(), core.Payment{ID: 1, Amount: 100})
	if err == nil || strings.Contains(err.Error(), "wrong-secret") {
		t.Errorf("ошибка не та: %v", err)
	}
}

func refundHook(paymentID string) *http.Request {
	body := fmt.Sprintf(`{"type":"notification","event":"refund.succeeded","object":{"id":"rf-1","payment_id":%q}}`, paymentID)
	return httptest.NewRequest(http.MethodPost, "/pay/yookassa", strings.NewReader(body))
}

func TestRefund(t *testing.T) {
	f := newFakeAPI(t)
	g := newGateway(f)
	f.set("full", remote{status: "succeeded", value: "5.00", meta: "9", refunded: "5.00"})
	f.set("part", remote{status: "succeeded", value: "5.00", meta: "9", refunded: "1.00"})
	ev, err := g.Verify(refundHook("full"))
	if err != nil || ev.Status != core.PayRefunded || ev.ExternalID != "full" {
		t.Fatalf("полный возврат: %+v %v", ev, err)
	}
	if ev, err := g.Verify(refundHook("part")); err != nil || ev.Status != core.PayPending {
		t.Fatalf("частичный возврат не должен отменять зачисление: %+v %v", ev, err)
	}
	if _, err := g.Verify(refundHook("")); err != pay.ErrBadSignature {
		t.Fatalf("возврат без платежа: %v", err)
	}
}
