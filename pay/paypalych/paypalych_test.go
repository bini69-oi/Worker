package paypalych

import (
	"context"
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

const (
	testToken = "tok-s3cret"
	testShop  = "shop-77"
	testBill  = "Vq3Lk9"
)

type cfg map[string]string

func (c cfg) String(k string) string { return c[k] }
func (c cfg) Secret(k string) string { return c[k] }
func (c cfg) Int(k string) int64 {
	n, _ := strconv.ParseInt(c[k], 10, 64)
	return n
}

func good() cfg { return cfg{keyToken: testToken, keyShop: testShop} }

func data(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// fakeAPI изображает API PayPalych: проверяет токен и магазин, отдаёт записанные ответы.
func fakeAPI(t *testing.T, statusFile string) *httptest.Server {
	t.Helper()
	create, status, denied := data(t, "create_ok.json"), data(t, statusFile), data(t, "error_401.json")
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+testToken {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write(denied)
			return
		}
		switch r.URL.Path {
		case "/bill/create":
			var in map[string]string
			if err := jsonBody(r, &in); err != nil || in["shop_id"] != testShop || in["amount"] != "300.00" ||
				!strings.HasPrefix(in["order_id"], orderPrefix) || !strings.HasPrefix(in["success_url"], "https://bot.test/paid/") {
				http.Error(w, "bad", http.StatusBadRequest)
				return
			}
			_, _ = w.Write(create)
		case "/bill/status":
			if r.URL.Query().Get("id") != testBill {
				http.NotFound(w, r)
				return
			}
			_, _ = w.Write(status)
		default:
			http.NotFound(w, r)
		}
	}))
}

// hook собирает postback в форме. Пустая подпись означает верную.
func hook(p core.Payment, amount int64, token, sig string) *http.Request {
	order, out := orderPrefix+strconv.FormatInt(p.ID, 10), rubles(amount)
	if sig == "" {
		sig = sign(out, order, token)
	}
	body := string(data1("postback_paid.form"))
	body = strings.NewReplacer("{{ORDER}}", url.QueryEscape(order), "{{AMOUNT}}", out, "{{SIGN}}", sig).Replace(body)
	r := httptest.NewRequest(http.MethodPost, "/pay/paypalych", strings.NewReader(strings.TrimSpace(body)))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return r
}

func data1(name string) []byte {
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		panic(err)
	}
	return b
}

func TestGateway(t *testing.T) {
	api := fakeAPI(t, "status_success.json")
	defer api.Close()
	g := New(good(), api.URL, api.Client())
	g.Return = func(id int64) string { return "https://bot.test/paid/" + strconv.FormatInt(id, 10) }
	gatewaytest.Run(t, gatewaytest.Case{
		Gateway:      g,
		Unconfigured: New(cfg{}, api.URL, api.Client()),
		Amount:       30000,
		Paid:         func(p core.Payment, a int64) *http.Request { return hook(p, a, testToken, "") },
		Forged:       func(p core.Payment, a int64) *http.Request { return hook(p, a, "чужой-токен", "") },
	})
}

func TestSignature(t *testing.T) {
	g := New(good(), "", nil)
	p := core.Payment{ID: 5}
	lower := hook(p, 30000, testToken, strings.ToLower(sign("300.00", "worker:5", testToken)))
	ev, err := g.Verify(lower)
	if err != nil || ev.PaymentID != 5 || ev.ExtAmount != 30000 || ev.Status != core.PayPaid {
		t.Fatalf("подпись в нижнем регистре: %+v %v", ev, err)
	}
	if _, err := g.Verify(hook(p, 30000, testToken, "DEADBEEF")); err == nil {
		t.Fatal("чужая подпись принята")
	}
	if _, err := New(cfg{}, "", nil).Verify(hook(p, 30000, "", "")); err == nil {
		t.Fatal("касса без токена приняла postback")
	}
}

func TestPostbackShapes(t *testing.T) {
	g := New(good(), "", nil)
	// InvId как номер счёта кассы, JSON вместо формы.
	body := `{"Status":"FAIL","InvId":"` + testBill + `","OutSum":300,"SignatureValue":"` + sign("300", testBill, testToken) + `"}`
	r := httptest.NewRequest(http.MethodPost, "/pay/paypalych", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	ev, err := g.Verify(r)
	if err != nil || ev.ExternalID != testBill || ev.Status != core.PayFailed || ev.ExtAmount != 30000 {
		t.Fatalf("json postback: %+v %v", ev, err)
	}
}

func TestStatus(t *testing.T) {
	for file, want := range map[string]core.PayStatus{"status_success.json": core.PayPaid, "status_new.json": core.PayPending} {
		api := fakeAPI(t, file)
		g := New(good(), api.URL, api.Client())
		ev, err := g.Status(context.Background(), core.Payment{ID: 1, ExternalID: testBill})
		api.Close()
		if err != nil || ev.Status != want || (want == core.PayPaid && ev.ExtAmount != 30000) {
			t.Fatalf("%s: %+v %v", file, ev, err)
		}
	}
}

func TestErrorsHideSecrets(t *testing.T) {
	api := fakeAPI(t, "status_new.json")
	defer api.Close()
	bad := cfg{keyToken: "wrong-" + testToken, keyShop: testShop}
	g := New(bad, api.URL, api.Client())
	_, err := g.Create(context.Background(), core.Payment{ID: 1, Amount: 30000})
	if err == nil || strings.Contains(err.Error(), "wrong-") || strings.Contains(err.Error(), testShop) {
		t.Fatalf("ошибка пустая или с секретом: %v", err)
	}
}

func TestMoney(t *testing.T) {
	for in, want := range map[string]int64{"300": 30000, "300.5": 30050, "0.01": 1, "300.000": 30000} {
		if got, err := kopecks(in); err != nil || got != want {
			t.Errorf("%s: %d %v", in, got, err)
		}
	}
	for _, in := range []string{"", "abc", "1.005", "-1"} {
		if _, err := kopecks(in); err == nil {
			t.Errorf("%q принято", in)
		}
	}
}

func jsonBody(r *http.Request, v any) error { return json.NewDecoder(r.Body).Decode(v) }
