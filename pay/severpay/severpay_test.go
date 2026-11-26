package severpay

import (
	"context"
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

var full = cfg{keyMID: "77", keyToken: "tok-test"}

func read(t testing.TB, name string) string {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// fakeAPI проверяет подпись запроса так же, как это делает касса, и отдаёт записанные ответы.
func fakeAPI(t *testing.T, getFile string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var top map[string]json.RawMessage
		var got string
		if json.Unmarshal(raw, &top) != nil || json.Unmarshal(top["sign"], &got) != nil ||
			!equalSign(got, mac(canon(top, false), "tok-test")) {
			_, _ = w.Write([]byte(read(t, "create_error.json")))
			return
		}
		if string(top["mid"]) != "77" || len(top["salt"]) < 10 {
			t.Errorf("нет mid или salt: %s", raw)
		}
		switch r.URL.Path {
		case "/payin/create":
			if string(top["amount"]) != "300.5" || !strings.HasPrefix(string(top["order_id"]), `"`+orderPrefix) {
				t.Errorf("платёж создан не так: %s", raw)
			}
			_, _ = w.Write([]byte(read(t, "create_ok.json")))
		case "/payin/get":
			_, _ = w.Write([]byte(read(t, getFile)))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// hook собирает вебхук: сумма и номер подставлены в записанный образец, подпись пересчитана ключом key.
func hook(p core.Payment, amount int64, key string) *http.Request {
	var top map[string]json.RawMessage
	_ = json.Unmarshal([]byte(mustRead("webhook_paid.json")), &top)
	data := strings.NewReplacer("worker_7", orderPrefix+strconv.FormatInt(p.ID, 10), "300.0", rubles(amount)).
		Replace(string(top["data"]))
	top["data"] = json.RawMessage(data)
	body := signed(top, key)
	return httptest.NewRequest(http.MethodPost, "/pay/severpay", strings.NewReader(string(body)))
}

func mustRead(name string) string {
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		panic(err)
	}
	return string(b)
}

func TestSuite(t *testing.T) {
	srv := fakeAPI(t, "get_paid.json")
	gatewaytest.Run(t, gatewaytest.Case{
		Gateway:      New(full, srv.URL, srv.Client()),
		Unconfigured: New(cfg{}, srv.URL, srv.Client()),
		Amount:       30050,
		Paid:         func(p core.Payment, a int64) *http.Request { return hook(p, a, "tok-test") },
		Forged:       func(p core.Payment, a int64) *http.Request { return hook(p, a, "чужой-ключ") },
	})
}

func TestCreate(t *testing.T) {
	srv := fakeAPI(t, "get_paid.json")
	inv, err := New(full, srv.URL, srv.Client()).Create(context.Background(), core.Payment{ID: 1, UserID: 5, Amount: 30050})
	if err != nil {
		t.Fatal(err)
	}
	if inv.ExternalID != "4815" || !strings.HasPrefix(inv.URL, "https://pay.severpay.test/") || inv.ExtAmount != 30050 || inv.ExtCurrency != "RUB" {
		t.Fatalf("счёт не тот: %+v", inv)
	}
}

func TestCreateWrongKey(t *testing.T) {
	srv := fakeAPI(t, "get_paid.json")
	bad := cfg{keyMID: "77", keyToken: "tok-wrong"}
	_, err := New(bad, srv.URL, srv.Client()).Create(context.Background(), core.Payment{ID: 1, Amount: 30050})
	if err == nil || strings.Contains(err.Error(), "tok-wrong") {
		t.Fatalf("ждали ошибку без ключа, получили %v", err)
	}
}

func TestStatus(t *testing.T) {
	for _, tc := range []struct {
		file   string
		status core.PayStatus
		amount int64
	}{
		{"get_paid.json", core.PayPaid, 30000},
		{"get_pending.json", core.PayPending, 0},
	} {
		srv := fakeAPI(t, tc.file)
		ev, err := New(full, srv.URL, srv.Client()).Status(context.Background(), core.Payment{ID: 1, ExternalID: "4815"})
		if err != nil || ev.Status != tc.status || ev.ExtAmount != tc.amount {
			t.Fatalf("%s: %+v %v", tc.file, ev, err)
		}
	}
}

// TestVerifyRecorded проверяет вебхук, подписанный Python-образцом (кириллица, число 300.0 как есть).
func TestVerifyRecorded(t *testing.T) {
	g := New(full, "", nil)
	r := httptest.NewRequest(http.MethodPost, "/pay/severpay", strings.NewReader(mustRead("webhook_paid.json")))
	ev, err := g.Verify(r)
	if err != nil || ev.PaymentID != 7 || ev.ExternalID != "4815" || ev.ExtAmount != 30000 || ev.Status != core.PayPaid {
		t.Fatalf("%+v %v", ev, err)
	}
	// тело изменено после подписи
	bad := strings.Replace(mustRead("webhook_paid.json"), `"amount":300.0`, `"amount":900.0`, 1)
	r2 := httptest.NewRequest(http.MethodPost, "/pay/severpay", strings.NewReader(bad))
	if _, err := g.Verify(r2); err != pay.ErrBadSignature {
		t.Fatalf("ждали ErrBadSignature, получили %v", err)
	}
}

func TestVerifyNoSign(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/pay/severpay", strings.NewReader(`{"type":"payin","data":{"id":1}}`))
	if _, err := New(full, "", nil).Verify(r); err != pay.ErrBadSignature {
		t.Fatalf("ждали ErrBadSignature, получили %v", err)
	}
}

func TestMoney(t *testing.T) {
	for kop, want := range map[int64]string{30000: "300.0", 30050: "300.5", 12345: "123.45", 5: "0.05"} {
		if got := rubles(kop); got != want {
			t.Errorf("rubles(%d) = %s, ждали %s", kop, got, want)
		}
	}
	if _, err := kopecks("1.005"); err == nil {
		t.Error("доли копейки должны давать ошибку")
	}
}

func TestAck(t *testing.T) {
	if New(full, "", nil).Ack(core.GatewayEvent{}) != `{"status":true}` {
		t.Fatal("не то слово в ответе")
	}
}
