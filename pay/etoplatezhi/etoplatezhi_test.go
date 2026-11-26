package etoplatezhi

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha512"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
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

var full = cfg{keyProject: "4242", keySecret: "secret-test", keyMethod: "sbp"}

func read(t testing.TB, name string) string {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// fakeAPI проверяет подпись запроса и отдаёт записанные ответы. Платёж не из нашего магазина это ошибка кассы.
func fakeAPI(t *testing.T, statusFile string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		dec := json.NewDecoder(r.Body)
		dec.UseNumber()
		_ = dec.Decode(&req)
		general, _ := req["general"].(map[string]any)
		got, _ := general["signature"].(string)
		id, _ := general["payment_id"].(string)
		if r.URL.Path != "/v2/payment/status" || got != sign(req, "secret-test") || general["project_id"] != json.Number("4242") ||
			!strings.HasPrefix(id, orderPrefix) {
			_, _ = w.Write([]byte(read(t, "status_error.json")))
			return
		}
		_, _ = w.Write([]byte(strings.ReplaceAll(read(t, statusFile), "{{AMOUNT}}", "30000")))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// hook собирает вебхук из записанного образца и подписывает его ключом key.
func hook(t testing.TB, id, status string, amount int64, key string) *http.Request {
	t.Helper()
	body := strings.NewReplacer("{{ID}}", id, "{{STATUS}}", status, "{{AMOUNT}}", strconv.FormatInt(amount, 10)).
		Replace(read(t, "webhook.tmpl"))
	dec := json.NewDecoder(strings.NewReader(body))
	dec.UseNumber()
	var m map[string]any
	if err := dec.Decode(&m); err != nil {
		t.Fatal(err)
	}
	m["signature"] = sign(m, key)
	b, _ := json.Marshal(m)
	return httptest.NewRequest(http.MethodPost, "/pay/etoplatezhi", bytes.NewReader(b))
}

func TestSuite(t *testing.T) {
	srv := fakeAPI(t, "status_paid.json")
	gatewaytest.Run(t, gatewaytest.Case{
		Gateway:      New(full, srv.URL, srv.Client()),
		Unconfigured: New(cfg{}, srv.URL, srv.Client()),
		Paid: func(p core.Payment, a int64) *http.Request {
			return hook(t, p.ExternalID, "success", a, "secret-test")
		},
		Forged: func(p core.Payment, a int64) *http.Request {
			return hook(t, p.ExternalID, "success", a, "чужой-ключ")
		},
		Amount: 30000,
	})
}

func TestCreate(t *testing.T) {
	g := New(full, "", nil)
	g.Return, g.Hook = func(id int64) string { return "https://t.me/bot?start=paid_" + strconv.FormatInt(id, 10) }, "https://w.example/pay/etoplatezhi"
	inv, err := g.Create(t.Context(), core.Payment{ID: 7, UserID: 1001, Amount: 30000})
	if err != nil || inv.ExternalID != "worker_7" || inv.ExtAmount != 30000 {
		t.Fatalf("%+v %v", inv, err)
	}
	u, err := url.Parse(inv.URL)
	if err != nil || !strings.HasPrefix(inv.URL, pageURL+"?") {
		t.Fatalf("ссылка: %s %v", inv.URL, err)
	}
	q := u.Query()
	if q.Get("payment_amount") != "30000" || q.Get("force_payment_method") != "sbp" || q.Get("project_id") != "4242" ||
		q.Get("merchant_callback_url") != g.Hook || q.Get("redirect_success_url") == "" {
		t.Fatalf("параметры: %v", q)
	}
	params := map[string]any{}
	for k := range q {
		params[k] = q.Get(k)
	}
	if q.Get("signature") != sign(params, "secret-test") {
		t.Fatal("подпись ссылки не сходится")
	}
	if _, err := New(cfg{keyProject: "abc", keySecret: "s"}, "", nil).Create(t.Context(), core.Payment{ID: 1}); err == nil {
		t.Fatal("нечисловой project_id принят")
	}
}

// Подпись по описанию кассы: плоские пары, сортировка, «;», HMAC-SHA512, base64.
func TestSignAlgorithm(t *testing.T) {
	params := map[string]any{
		"project_id": json.Number("1"), "ok": true, "no": false, "empty": []any{}, "frame_mode": "1",
		"payment": map[string]any{"id": "a", "tags": []any{"x", "y"}, "signature": "z"},
	}
	mac := hmac.New(sha512.New, []byte("k"))
	mac.Write([]byte("no:0;ok:1;payment:id:a;payment:tags:0:x;payment:tags:1:y;project_id:1"))
	if want := base64.StdEncoding.EncodeToString(mac.Sum(nil)); sign(params, "k") != want {
		t.Fatal("подпись не по правилу кассы")
	}
}

func TestStatus(t *testing.T) {
	for file, want := range map[string]core.PayStatus{
		"status_paid.json": core.PayPaid, "status_pending.json": core.PayPending,
		"status_refunded.json": core.PayRefunded, "status_declined.json": core.PayFailed,
	} {
		srv := fakeAPI(t, file)
		ev, err := New(full, srv.URL, srv.Client()).Status(t.Context(), core.Payment{ExternalID: "worker_1"})
		if err != nil || ev.Status != want {
			t.Fatalf("%s: %+v %v", file, ev, err)
		}
		if want == core.PayPaid && ev.ExtAmount != 30000 {
			t.Fatalf("сумма %d", ev.ExtAmount)
		}
	}
	srv := fakeAPI(t, "status_paid.json")
	if _, err := New(full, srv.URL, srv.Client()).Status(t.Context(), core.Payment{ExternalID: "чужой"}); err == nil {
		t.Fatal("неизвестный платёж дал статус")
	}
}

// Вебхук подписан ключом проекта: оплата принимается по нему, даже если API кассы недоступен.
func TestSignedHookWithoutAPI(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "down", 500) }))
	defer srv.Close()
	ev, err := New(full, srv.URL, srv.Client()).Verify(hook(t, "worker_1", "success", 30000, "secret-test"))
	if err != nil || ev.Status != core.PayPaid || ev.ExtAmount != 30000 || ev.PaymentID != 1 {
		t.Fatalf("подписанный вебхук: %+v %v", ev, err)
	}
}

func TestBadHooks(t *testing.T) {
	srv := fakeAPI(t, "status_paid.json")
	g := New(full, srv.URL, srv.Client())
	for name, body := range map[string]string{"не JSON": "x", "без подписи": `{"payment":{"id":"worker_1","status":"success"}}`} {
		if _, err := g.Verify(httptest.NewRequest(http.MethodPost, "/pay/etoplatezhi", strings.NewReader(body))); err == nil {
			t.Errorf("%s: вебхук принят", name)
		}
	}
	if _, err := New(cfg{}, srv.URL, srv.Client()).Verify(hook(t, "worker_1", "success", 30000, "")); err == nil {
		t.Error("вебхук принят без ключей")
	}
	ev, err := g.Verify(hook(t, "worker_1", "success", 29999, "secret-test"))
	if err != nil || ev.ExtAmount != 29999 {
		t.Errorf("чужая сумма не помечена: %+v %v", ev, err)
	}
	// Промежуточный статус подтверждения не требует: ядро ничего не зачислит.
	if ev, err := g.Verify(hook(t, "worker_1", "processing", 30000, "secret-test")); err != nil || ev.Status != core.PayPending {
		t.Errorf("processing: %+v %v", ev, err)
	}
}
