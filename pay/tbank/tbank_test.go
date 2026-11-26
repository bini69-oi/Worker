package tbank

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"testing"

	"github.com/bini69-oi/worker/core"
	"github.com/bini69-oi/worker/pay/gatewaytest"
)

type cfg map[string]string

func (c cfg) Secret(k string) string { return c[k] }
func (c cfg) String(k string) string { return c[k] }

var full = cfg{keyTerminal: "term", keyPassword: "pass"}

func read(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// fakeAPI изображает Init и GetState: проверяет Token и отдаёт записанный образец.
func fakeAPI(t *testing.T, states map[string]string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		dec := json.NewDecoder(r.Body)
		dec.UseNumber()
		if err := dec.Decode(&req); err != nil || req["Token"] != token(req, "pass") {
			http.Error(w, "bad token", http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/Init":
			name := "init_ok.json"
			if req["TerminalKey"] != "term" {
				name = "init_error.json"
			}
			_, _ = w.Write(read(t, name))
		case "/GetState":
			id, _ := req["PaymentId"].(json.Number)
			_, _ = w.Write(read(t, states[id.String()]))
		default:
			http.NotFound(w, r)
		}
	})
}

// hook собирает уведомление по образцу testdata/notification.json и подписывает его.
func hook(t *testing.T, id int64, amount int64, status, pass string) *http.Request {
	t.Helper()
	var f map[string]any
	dec := json.NewDecoder(bytes.NewReader(read(t, "notification.json")))
	dec.UseNumber()
	if err := dec.Decode(&f); err != nil {
		t.Fatal(err)
	}
	f["OrderId"], f["Amount"], f["Status"] = strconv.FormatInt(id, 10), json.Number(strconv.FormatInt(amount, 10)), status
	f["Token"] = token(f, pass)
	body, _ := json.Marshal(f)
	r := httptest.NewRequest(http.MethodPost, "/pay/tbank", bytes.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	return r
}

func TestSuite(t *testing.T) {
	srv := httptest.NewServer(fakeAPI(t, nil))
	defer srv.Close()
	g := New(full, srv.URL, srv.Client())
	gatewaytest.Run(t, gatewaytest.Case{
		Gateway:      g,
		Unconfigured: New(cfg{}, srv.URL, srv.Client()),
		Amount:       30000,
		Paid: func(p core.Payment, a int64) *http.Request {
			return hook(t, p.ID, a, "CONFIRMED", "pass")
		},
		Forged: func(p core.Payment, a int64) *http.Request {
			return hook(t, p.ID, a, "CONFIRMED", "чужой")
		},
	})
}

// Пример из документации Т-Банка: пароль TinkoffBankTest, вложенные DATA и Receipt в подпись не входят.
func TestTokenDocExample(t *testing.T) {
	var f map[string]any
	raw := `{"TerminalKey":"TinkoffBankTest","Amount":"100000","OrderId":"TokenExample","Description":"test",
	"DATA":{"Phone":"+71234567890"},"Receipt":{"Items":[{"Name":"x","Price":10000}]}}`
	if err := json.Unmarshal([]byte(raw), &f); err != nil {
		t.Fatal(err)
	}
	const want = "48d4ca825aab2ede06736d3eae099bd56ac97bd1bcdd598aff210f729de4eb21"
	if got := token(f, "TinkoffBankTest"); got != want {
		t.Fatalf("Token %s", got)
	}
}

func TestTokenBool(t *testing.T) {
	// "100" + "true" + "p" по порядку имён Amount, Success, Password: булево идёт как слово.
	f := map[string]any{"Amount": json.Number("100"), "Success": true, "Skip": nil, "Token": "z"}
	if token(f, "p") != token(map[string]any{"Amount": "100", "Success": "true"}, "p") {
		t.Fatal("булево значение подписано не как true")
	}
}

func TestCreate(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		_, _ = w.Write(read(t, "init_ok.json"))
	}))
	defer srv.Close()
	g := New(cfg{keyTerminal: "term", keyPassword: "pass", keyReturn: "https://x/{payment}"}, srv.URL, srv.Client())
	g.Hook = "https://h/pay/tbank"
	inv, err := g.Create(context.Background(), core.Payment{ID: 7, Amount: 30050})
	if err != nil {
		t.Fatal(err)
	}
	if inv.ExternalID != "2304567" || inv.ExtAmount != 30050 || inv.URL == "" || got["Amount"] != float64(30050) ||
		got["OrderId"] != "7" || got["SuccessURL"] != "https://x/7" || got["NotificationURL"] != "https://h/pay/tbank" {
		t.Fatalf("счёт %+v, запрос %v", inv, got)
	}
	srv2 := httptest.NewServer(fakeAPI(t, nil))
	defer srv2.Close()
	bad := New(cfg{keyTerminal: "other", keyPassword: "pass"}, srv2.URL, srv2.Client())
	_, err = bad.Create(context.Background(), core.Payment{ID: 7, Amount: 100})
	if err == nil || bytes.Contains([]byte(err.Error()), []byte("pass")) {
		t.Fatalf("ошибка кассы: %v", err)
	}
}

func TestStatus(t *testing.T) {
	srv := httptest.NewServer(fakeAPI(t, map[string]string{"1": "state_confirmed.json", "2": "state_new.json",
		"3": "state_rejected.json", "4": "state_refunded.json", "5": "state_partial_refunded.json",
		"6": "state_authorized.json"}))
	defer srv.Close()
	g := New(full, srv.URL, srv.Client())
	for _, tc := range []struct {
		id     string
		status core.PayStatus
		cents  int64
	}{
		{"1", core.PayPaid, 30000}, {"2", core.PayPending, 0}, {"3", core.PayFailed, 0},
		{"4", core.PayRefunded, 30000}, {"5", core.PayPending, 0}, {"6", core.PayPending, 0},
	} {
		ev, err := g.Status(context.Background(), core.Payment{ID: 7, ExternalID: tc.id})
		if err != nil || ev.Status != tc.status || ev.ExtAmount != tc.cents {
			t.Fatalf("платёж %s: %+v, %v", tc.id, ev, err)
		}
	}
	if _, err := New(cfg{}, srv.URL, srv.Client()).Status(context.Background(), core.Payment{ExternalID: "1"}); err == nil {
		t.Fatal("статус без ключей")
	}
}

func TestVerifyTable(t *testing.T) {
	g := New(full, "", nil)
	for _, tc := range []struct {
		name, status, pass string
		wantErr            bool
		want               core.PayStatus
	}{
		{"оплачен", "CONFIRMED", "pass", false, core.PayPaid},
		{"холд", "AUTHORIZED", "pass", false, core.PayPending},
		{"отказ", "REJECTED", "pass", false, core.PayFailed},
		{"возврат", "REFUNDED", "pass", false, core.PayRefunded},
		{"частичный возврат", "PARTIAL_REFUNDED", "pass", false, core.PayPending},
		{"чужой пароль", "CONFIRMED", "x", true, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ev, err := g.Verify(hook(t, 7, 30000, tc.status, tc.pass))
			if (err != nil) != tc.wantErr || (!tc.wantErr && (ev.Status != tc.want || ev.PaymentID != 7 ||
				ev.ExternalID != "2304567" || g.Ack(ev) != "OK")) {
				t.Fatalf("событие %+v, ошибка %v", ev, err)
			}
		})
	}
	t.Run("подмена суммы", func(t *testing.T) {
		r := hook(t, 7, 30000, "CONFIRMED", "pass")
		body := bytes.Replace(readBody(r), []byte(`"Amount":30000`), []byte(`"Amount":1`), 1)
		r2 := httptest.NewRequest(http.MethodPost, "/pay/tbank", bytes.NewReader(body))
		if _, err := g.Verify(r2); err == nil {
			t.Fatal("подменённая сумма принята")
		}
	})
	t.Run("не JSON", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodPost, "/pay/tbank", bytes.NewReader([]byte("x=1")))
		if _, err := g.Verify(r); err == nil {
			t.Fatal("форма принята")
		}
	})
}

func readBody(r *http.Request) []byte {
	var b bytes.Buffer
	_, _ = b.ReadFrom(r.Body)
	return b.Bytes()
}
