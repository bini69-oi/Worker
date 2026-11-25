package cryptobot

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/bini69-oi/worker/core"
	"github.com/bini69-oi/worker/pay/gatewaytest"
)

const testToken = "123:AAtest"

type cfg map[string]string

func (c cfg) String(k string) string { return c[k] }
func (c cfg) Secret(k string) string { return c[k] }

// fakeAPI изображает Crypto Pay API: хранит счета и проверяет токен.
func fakeAPI(t *testing.T) *httptest.Server {
	t.Helper()
	invoices := map[int64]invoice{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Crypto-Pay-API-Token") != testToken {
			_, _ = w.Write([]byte(`{"ok":false,"error":{"code":401,"name":"UNAUTHORIZED"}}`))
			return
		}
		switch strings.TrimPrefix(r.URL.Path, "/") {
		case "createInvoice":
			var req map[string]string
			_ = json.NewDecoder(r.Body).Decode(&req)
			if req["currency_type"] != "fiat" || req["fiat"] != "RUB" || req["accepted_assets"] != "USDT,TON" {
				t.Errorf("счёт создан не так: %v", req)
			}
			id := int64(len(invoices) + 500)
			inv := invoice{ID: id, Status: "active", Payload: req["payload"], Amount: amount(req["amount"]), Fiat: "RUB", BotURL: "https://t.me/CryptoBot?start=IV" + strconv.FormatInt(id, 10)}
			invoices[id] = inv
			reply, _ := json.Marshal(map[string]any{"ok": true, "result": inv})
			_, _ = w.Write(reply)
		case "getInvoices":
			id, _ := strconv.ParseInt(r.URL.Query().Get("invoice_ids"), 10, 64)
			inv := invoices[id]
			inv.Status = "paid"
			reply, _ := json.Marshal(map[string]any{"ok": true, "result": map[string]any{"items": []invoice{inv}}})
			_, _ = w.Write(reply)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func hook(token string, p core.Payment, extAmount int64) *http.Request {
	id, _ := strconv.ParseInt(p.ExternalID, 10, 64)
	body, _ := json.Marshal(map[string]any{
		"update_id": 1, "update_type": "invoice_paid",
		"payload": invoice{ID: id, Status: "paid", Payload: strconv.FormatInt(p.ID, 10), Amount: amount(formatKopeks(extAmount)), Fiat: "RUB"},
	})
	r := httptest.NewRequest(http.MethodPost, "/pay/cryptobot", strings.NewReader(string(body)))
	r.Header.Set("crypto-pay-api-signature", hexSign(token, body))
	return r
}

func hexSign(token string, body []byte) string {
	const hexd = "0123456789abcdef"
	var b strings.Builder
	for _, c := range sign(token, body) {
		b.WriteByte(hexd[c>>4])
		b.WriteByte(hexd[c&15])
	}
	return b.String()
}

func TestGateway(t *testing.T) {
	srv := fakeAPI(t)
	good := cfg{keyToken: testToken, keyAssets: " usdt, ton "}
	gatewaytest.Run(t, gatewaytest.Case{
		Gateway:      New(good, srv.URL, srv.Client()),
		Unconfigured: New(cfg{}, srv.URL, srv.Client()),
		Paid:         func(p core.Payment, a int64) *http.Request { return hook(testToken, p, a) },
		Forged:       func(p core.Payment, a int64) *http.Request { return hook("чужой:токен", p, a) },
		Amount:       30000,
	})
}

func TestStatusAndErrors(t *testing.T) {
	srv := fakeAPI(t)
	g := New(cfg{keyToken: testToken, keyAssets: "USDT,TON"}, srv.URL, nil)
	inv, err := g.Create(t.Context(), core.Payment{ID: 7, Amount: 12345})
	if err != nil || inv.ExtAmount != 12345 {
		t.Fatalf("счёт: %+v %v", inv, err)
	}
	ev, err := g.Status(t.Context(), core.Payment{ID: 7, ExternalID: inv.ExternalID, ExtAmount: 12345})
	if err != nil || ev.Status != core.PayPaid || ev.ExtAmount != 12345 || ev.PaymentID != 7 {
		t.Fatalf("статус: %+v %v", ev, err)
	}
	bad := New(cfg{keyToken: "чужой"}, srv.URL, nil)
	if _, err := bad.Create(t.Context(), core.Payment{ID: 1, Amount: 100}); err == nil || strings.Contains(err.Error(), "чужой") {
		t.Fatalf("ждали отказ без токена в тексте: %v", err)
	}
}

func TestParseKopeks(t *testing.T) {
	ok := map[string]int64{"300.00": 30000, "123.4": 12340, "5": 500, "0.07": 7, "10.500": 1050}
	for in, want := range ok {
		if got, err := parseKopeks(in); err != nil || got != want {
			t.Errorf("%q: %d %v, ждали %d", in, got, err, want)
		}
	}
	for _, in := range []string{"", "abc", "-5", "1.2.3", "1e3", "10.501", ".", "1,5"} {
		if _, err := parseKopeks(in); err == nil {
			t.Errorf("%q: ждали ошибку", in)
		}
	}
}

func TestAmountStringOrNumber(t *testing.T) {
	for raw, want := range map[string]string{`{"amount":"300.00"}`: "300.00", `{"amount":300}`: "300", `{"amount":123.4}`: "123.4"} {
		var inv invoice
		if err := json.Unmarshal([]byte(raw), &inv); err != nil || string(inv.Amount) != want {
			t.Errorf("%s: получили %q (%v)", raw, inv.Amount, err)
		}
	}
}
