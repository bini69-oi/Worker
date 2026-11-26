package donut

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
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

var full = cfg{keyToken: "tok-test", keySecret: "sec-test", keyPhone: "79001234567", keyMethod: "SBP"}

const extID = "tx-7f3a91"

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
	g := New(full, "", nil)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Token         string `json:"token"`
			TransactionID string `json:"transaction_id"`
			OrderID       string `json:"order_id"`
			Redirect      string `json:"redirect"`
			Signature     string `json:"signature"`
			Description   string `json:"description"`
			Amount        struct {
				Value    string `json:"value"`
				Currency string `json:"currency"`
			} `json:"amount"`
			Customer struct {
				ID    string `json:"id"`
				Phone string `json:"phone"`
			} `json:"customer"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		switch r.URL.Path {
		case "/p2p_payin":
			sig := g.sign("token", req.Token, "order_id", req.OrderID, "amount.value", req.Amount.Value,
				"amount.currency", req.Amount.Currency, "customer.id", req.Customer.ID, "redirect", req.Redirect)
			if req.Signature != sig || req.Amount.Value != "300.00" || req.Description != "SBP" ||
				!strings.HasPrefix(req.OrderID, orderPrefix) {
				t.Errorf("платёж создан не так: %+v", req)
			}
			if req.Customer.Phone == "" {
				_, _ = w.Write([]byte(read(t, "create_error.json")))
				return
			}
			_, _ = w.Write([]byte(read(t, "create_ok.json")))
		case "/p2p_status":
			if req.Signature != g.sign("token", req.Token, "transaction_id", req.TransactionID) || req.TransactionID != extID {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			_, _ = w.Write([]byte(strings.ReplaceAll(read(t, statusFile), "{{AMOUNT}}", "300.00")))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// hook собирает вебхук, подписанный секретом key. Сумма в копейках, в теле рубли.
func hook(p core.Payment, amount int64, key string) *http.Request {
	signer := New(cfg{keySecret: key}, "", nil)
	order := orderPrefix + strconv.FormatInt(p.ID, 10)
	sig := signer.sign("token", "tok-test", "transaction_id", extID, "order_id", order,
		"amount.value", rubles(amount), "amount.currency", "RUB", "recalculated", "false", "status.type", "success")
	body := strings.NewReplacer("{{ORDER}}", order, "{{AMOUNT}}", rubles(amount), "{{SIG}}", sig).
		Replace(readFile("testdata/webhook.tmpl"))
	return httptest.NewRequest(http.MethodPost, "/pay/donut", strings.NewReader(body))
}

func TestSuite(t *testing.T) {
	srv := fakeAPI(t, "status_paid.json")
	gatewaytest.Run(t, gatewaytest.Case{
		Gateway:      New(full, srv.URL, srv.Client()),
		Unconfigured: New(cfg{}, srv.URL, srv.Client()),
		Paid:         func(p core.Payment, a int64) *http.Request { return hook(p, a, "sec-test") },
		Forged:       func(p core.Payment, a int64) *http.Request { return hook(p, a, "чужой") },
		Amount:       30000,
	})
}

func TestStatus(t *testing.T) {
	for file, want := range map[string]core.PayStatus{
		"status_paid.json": core.PayPaid, "status_pending.json": core.PayPending, "status_cancelled.json": core.PayFailed,
	} {
		srv := fakeAPI(t, file)
		ev, err := New(full, srv.URL, srv.Client()).Status(t.Context(), core.Payment{ExternalID: extID})
		if err != nil || ev.Status != want {
			t.Fatalf("%s: %+v %v", file, ev, err)
		}
		if want == core.PayPaid && ev.ExtAmount != 30000 {
			t.Fatalf("сумма %d", ev.ExtAmount)
		}
	}
}

// Без телефона Donut отказывает: ошибка понятная, ключей в ней нет.
func TestCreateNeedsPhone(t *testing.T) {
	srv := fakeAPI(t, "status_paid.json")
	noPhone := cfg{keyToken: "tok-test", keySecret: "sec-test", keyMethod: "SBP"}
	_, err := New(noPhone, srv.URL, srv.Client()).Create(t.Context(), core.Payment{ID: 1, UserID: 5, Amount: 30000})
	if err == nil || strings.Contains(err.Error(), "sec-test") {
		t.Fatalf("ошибка: %v", err)
	}
}

func TestBadWebhooks(t *testing.T) {
	g := New(full, "", nil)
	good := read(t, "webhook.tmpl")
	body := func(s string) *http.Request {
		return httptest.NewRequest(http.MethodPost, "/pay/donut", strings.NewReader(s))
	}
	foreignToken := strings.Replace(good, `"tok-test"`, `"tok-other"`, 1)
	cases := map[string]*http.Request{
		"без подписи":    body(`{"transaction_id":"x","status":{"type":"success"}}`),
		"не JSON":        body("<xml/>"),
		"чужой токен":    body(foreignToken),
		"пустая подпись": body(good),
	}
	for name, r := range cases {
		if _, err := g.Verify(r); err == nil {
			t.Errorf("%s: вебхук принят", name)
		}
	}
	if _, err := New(cfg{}, "", nil).Verify(hook(core.Payment{ID: 1}, 30000, "")); err == nil {
		t.Error("пустой секрет принят")
	}
}

func TestSecretNotInError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "sec-test", 500) }))
	defer srv.Close()
	_, err := New(full, srv.URL, srv.Client()).Status(t.Context(), core.Payment{ExternalID: "x"})
	if err == nil || strings.Contains(err.Error(), "sec-test") || strings.Contains(err.Error(), "tok-test") {
		t.Fatalf("ошибка: %v", err)
	}
}

func TestKopecks(t *testing.T) {
	ok := map[string]int64{"300": 30000, "300.5": 30050, "300.50": 30050, "0.01": 1}
	for in, want := range ok {
		if got, err := kopecks(in); err != nil || got != want {
			t.Errorf("%q: %d %v", in, got, err)
		}
	}
	for _, in := range []string{"", "abc", "-1", "1.005"} {
		if _, err := kopecks(in); err == nil {
			t.Errorf("%q принято", in)
		}
	}
}

func readFile(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		panic(err)
	}
	return string(b)
}
