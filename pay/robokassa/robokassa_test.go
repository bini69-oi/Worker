package robokassa

import (
	"context"
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

func (c cfg) Secret(k string) string { return c[k] }
func (c cfg) String(k string) string { return c[k] }
func (c cfg) Bool(k string) bool     { return c[k] == "true" }

var full = cfg{keyLogin: "shop", keyPass1: "one", keyPass2: "two"}

// fakeAPI изображает OpState: проверяет подпись и отдаёт записанный образец по номеру счёта.
func fakeAPI(t *testing.T, files map[string]string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		want, _ := New(full, "", nil).sign("shop", q.Get("InvoiceID"), "two")
		if r.URL.Path != "/OpState" || q.Get("Signature") != want {
			http.Error(w, "bad", http.StatusUnauthorized)
			return
		}
		raw, err := os.ReadFile("testdata/" + files[q.Get("InvoiceID")])
		if err != nil {
			t.Error(err)
		}
		_, _ = w.Write(raw)
	})
}

// hook собирает ResultURL по образцу testdata/notification.form. sum это строка OutSum как её шлёт касса.
func hook(t *testing.T, g *Gateway, id int64, sum, extra string, forge bool) *http.Request {
	t.Helper()
	raw, err := os.ReadFile("testdata/notification.form")
	if err != nil {
		t.Fatal(err)
	}
	form, _ := url.ParseQuery(strings.TrimSpace(string(raw)))
	order := strconv.FormatInt(id, 10)
	form.Set("OutSum", sum)
	form.Set("InvId", order)
	parts := []string{sum, order, "two"}
	if extra != "" {
		k, v, _ := strings.Cut(extra, "=")
		form.Set(k, v)
		parts = append(parts, extra)
	}
	if forge {
		parts[2] = "чужой"
	}
	sig, _ := g.sign(parts...)
	form.Set("SignatureValue", strings.ToUpper(sig)) // касса шлёт подпись в любом регистре
	r := httptest.NewRequest(http.MethodPost, "/pay/robokassa", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
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
			return hook(t, g, p.ID, formatMoney(a)+".000000", "", false)
		},
		Forged: func(p core.Payment, a int64) *http.Request {
			return hook(t, g, p.ID, formatMoney(a), "", true)
		},
	})
}

func TestSignKnown(t *testing.T) {
	// Значения посчитаны внешним md5, не нашим кодом.
	g := New(full, "", nil)
	if s, _ := g.sign("300.000000", "7", "two"); s != "795114170e2b63c4fd2e27e95ba05697" {
		t.Fatalf("md5: %s", s)
	}
	if s, _ := g.sign("300.000000", "7", "two", "Shp_a=1"); s != "069fbfbbaffaa0d3f97bed050e1c3c2e" {
		t.Fatalf("md5 с Shp: %s", s)
	}
}

func TestVerifyTable(t *testing.T) {
	g := New(full, "", nil)
	for _, tc := range []struct {
		name, sum, extra string
		forge, wantErr   bool
		cents            int64
	}{
		{name: "шесть знаков", sum: "300.000000", cents: 30000},
		{name: "целая сумма", sum: "300", cents: 30000},
		{name: "копейки", sum: "300.50", cents: 30050},
		{name: "с Shp", sum: "300.00", extra: "Shp_a=1", cents: 30000},
		{name: "чужой пароль", sum: "300", forge: true, wantErr: true},
		{name: "доли копейки", sum: "300.001", wantErr: true},
		{name: "отрицательная", sum: "-5", wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ev, err := g.Verify(hook(t, g, 7, tc.sum, tc.extra, tc.forge))
			if (err != nil) != tc.wantErr {
				t.Fatalf("ошибка: %v", err)
			}
			if !tc.wantErr && (ev.ExtAmount != tc.cents || ev.PaymentID != 7 || g.Ack(ev) != "OK7") {
				t.Fatalf("событие: %+v", ev)
			}
		})
	}
	t.Run("подмена суммы", func(t *testing.T) {
		r := hook(t, g, 7, "300", "", false)
		_ = r.ParseForm()
		r.Form.Set("OutSum", "1")
		r.PostForm.Set("OutSum", "1")
		if _, err := g.Verify(r); err == nil {
			t.Fatal("подменённая сумма принята")
		}
	})
	t.Run("sha256", func(t *testing.T) {
		g2 := New(cfg{keyLogin: "shop", keyPass1: "one", keyPass2: "two", keyHash: "SHA256"}, "", nil)
		if _, err := g2.Verify(hook(t, g2, 7, "300", "", false)); err != nil {
			t.Fatal(err)
		}
		if _, err := g2.Verify(hook(t, g, 7, "300", "", false)); err == nil {
			t.Fatal("подпись md5 принята при sha256")
		}
	})
	t.Run("неизвестный алгоритм", func(t *testing.T) {
		g2 := New(cfg{keyLogin: "shop", keyPass1: "one", keyPass2: "two", keyHash: "crc32"}, "", nil)
		if g2.Configured() {
			t.Fatal("касса с неизвестным алгоритмом настроена")
		}
	})
}

func TestCreateLink(t *testing.T) {
	c := cfg{keyLogin: "shop", keyPass1: "one", keyPass2: "two", keyTest: "true"}
	inv, err := New(c, "", nil).Create(context.Background(), core.Payment{ID: 7, Amount: 30050})
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(inv.URL)
	q := u.Query()
	want, _ := New(c, "", nil).sign("shop", "300.50", "7", "one")
	if u.Host != "auth.robokassa.ru" || q.Get("OutSum") != "300.50" || q.Get("InvId") != "7" ||
		q.Get("SignatureValue") != want || q.Get("IsTest") != "1" || inv.ExternalID != "7" {
		t.Fatalf("ссылка: %s", inv.URL)
	}
	inv, _ = New(full, "", nil).Create(context.Background(), core.Payment{ID: 8, Amount: 30000})
	if !strings.Contains(inv.URL, "OutSum=300&") || strings.Contains(inv.URL, "IsTest") {
		t.Fatalf("ссылка: %s", inv.URL)
	}
}

func TestStatus(t *testing.T) {
	srv := httptest.NewServer(fakeAPI(t, map[string]string{"1": "opstate_paid.xml", "2": "opstate_new.xml",
		"3": "opstate_cancelled.xml", "4": "opstate_notfound.xml"}))
	defer srv.Close()
	g := New(full, srv.URL, srv.Client())
	for _, tc := range []struct {
		id      string
		status  core.PayStatus
		cents   int64
		wantErr bool
	}{
		{"1", core.PayPaid, 30000, false},
		{"2", core.PayPending, 0, false},
		{"3", core.PayFailed, 0, false},
		{"4", "", 0, true},
	} {
		ev, err := g.Status(context.Background(), core.Payment{ExternalID: tc.id})
		if (err != nil) != tc.wantErr || (!tc.wantErr && (ev.Status != tc.status || ev.ExtAmount != tc.cents)) {
			t.Fatalf("счёт %s: %+v, %v", tc.id, ev, err)
		}
	}
	if _, err := New(cfg{}, srv.URL, srv.Client()).Status(context.Background(), core.Payment{ExternalID: "1"}); err == nil {
		t.Fatal("статус без ключей")
	}
}
