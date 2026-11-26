package yoomoney

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/bini69-oi/worker/core"
	"github.com/bini69-oi/worker/pay"
	"github.com/bini69-oi/worker/pay/gatewaytest"
)

const testSecret = "s3cr3tNotify"

type cfg map[string]string

func (c cfg) String(k string) string { return c[k] }
func (c cfg) Secret(k string) string { return c[k] }

var full = cfg{"yoomoney.wallet": "4100111", "yoomoney.notification_secret": testSecret,
	"yoomoney.token": "tok-api", "yoomoney.payment_type": "AC"}

// fakeAPI изображает operation-history: отдаёт заданные операции, если токен верный.
func fakeAPI(ops []map[string]any) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/operation-history" || r.Header.Get("Authorization") != "Bearer tok-api" {
			http.Error(w, "no", http.StatusUnauthorized)
			return
		}
		_ = r.ParseForm()
		var out []map[string]any
		for _, op := range ops {
			if op["label"] == r.PostForm.Get("label") {
				out = append(out, op)
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"operations": out})
	})
}

// hook собирает подписанное уведомление. extra перекрывает поля после подписи полей из документации.
func hook(label string, withdraw int64, secret string, extra url.Values) *http.Request {
	f := url.Values{"notification_type": {"card-incoming"}, "operation_id": {"777"}, "amount": {rubles(withdraw - withdraw/200)},
		"currency": {rubleCode}, "datetime": {"2026-11-27T10:15:00Z"}, "sender": {""}, "codepro": {"false"},
		"label": {label}, "withdraw_amount": {rubles(withdraw)}}
	for k, v := range extra {
		f[k] = v
	}
	f.Set("sha1_hash", signature(f, secret))
	return form(f.Encode())
}

// upper переводит sha1_hash уведомления в верхний регистр: касса может прислать и так.
func upper(r *http.Request) *http.Request {
	_ = r.ParseForm()
	f := r.PostForm
	f.Set("sha1_hash", strings.ToUpper(f.Get("sha1_hash")))
	return form(f.Encode())
}

func form(body string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/pay/yoomoney", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return r
}

func TestSuite(t *testing.T) {
	srv := httptest.NewServer(fakeAPI(nil))
	defer srv.Close()
	gatewaytest.Run(t, gatewaytest.Case{
		Gateway:      New(full, srv.URL, srv.Client()),
		Unconfigured: New(cfg{}, srv.URL, srv.Client()),
		Amount:       30000,
		Paid: func(p core.Payment, amount int64) *http.Request {
			return hook(labelOf(p.ID), amount, testSecret, nil)
		},
		Forged: func(p core.Payment, amount int64) *http.Request {
			return hook(labelOf(p.ID), amount, "чужой-секрет", nil)
		},
	})
}

func TestVerifyTable(t *testing.T) {
	g := New(full, "", nil)
	read := func(name string) string {
		b, err := os.ReadFile("testdata/" + name + ".form")
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	cases := []struct {
		name    string
		req     *http.Request
		badSig  bool
		wantErr bool
		status  core.PayStatus
		amount  int64
	}{
		{name: "записанный p2p", req: form(read("p2p")), status: core.PayPaid, amount: 30000},
		{name: "записанная карта: берём withdraw_amount, не amount", req: form(read("card")), status: core.PayPaid, amount: 30000},
		{name: "записанный codepro не зачисляется", req: form(read("codepro")), status: core.PayPending},
		{name: "подмена amount ломает подпись", req: form(strings.Replace(read("p2p"), "amount=300.00", "amount=999.00", 1)), badSig: true},
		{name: "нет sha1_hash", req: form("notification_type=p2p-incoming&label=worker-7"), badSig: true},
		{name: "подпись другим секретом", req: hook("worker-7", 30000, "иной", nil), badSig: true},
		{name: "подпись в верхнем регистре", req: upper(hook("worker-7", 30000, testSecret, nil)), status: core.PayPaid, amount: 30000},
		{name: "unaccepted", req: hook("worker-7", 30000, testSecret, url.Values{"unaccepted": {"true"}}), status: core.PayPending},
		{name: "тестовое уведомление", req: hook("worker-7", 30000, testSecret, url.Values{"test_notification": {"true"}}), status: core.PayPending},
		{name: "codepro true", req: hook("worker-7", 30000, testSecret, url.Values{"codepro": {"true"}}), status: core.PayPending},
		{name: "не рубли", req: hook("worker-7", 30000, testSecret, url.Values{"currency": {"840"}}), wantErr: true},
		{name: "чужой тип", req: hook("worker-7", 30000, testSecret, url.Values{"notification_type": {"other"}}), wantErr: true},
		{name: "чужой label", req: hook("shop-9", 30000, testSecret, nil), wantErr: true},
		{name: "пустой label", req: hook("", 30000, testSecret, nil), wantErr: true},
		{name: "без withdraw_amount берём amount", req: hook("worker-7", 30000, testSecret, url.Values{"withdraw_amount": {""}, "amount": {"300.00"}}), status: core.PayPaid, amount: 30000},
		{name: "нулевая сумма", req: hook("worker-7", 30000, testSecret, url.Values{"withdraw_amount": {"0.00"}}), wantErr: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ev, err := g.Verify(c.req)
			switch {
			case c.badSig:
				if !errors.Is(err, pay.ErrBadSignature) {
					t.Fatalf("ждали ErrBadSignature, получили %v", err)
				}
			case c.wantErr:
				if err == nil || errors.Is(err, pay.ErrBadSignature) {
					t.Fatalf("ждали ошибку разбора, получили %v", err)
				}
			default:
				if err != nil {
					t.Fatal(err)
				}
				if ev.Status != c.status || ev.ExtAmount != c.amount || ev.PaymentID != 7 || ev.ExternalID != "worker-7" {
					t.Fatalf("событие не то: %+v", ev)
				}
			}
		})
	}
}

func TestVerifyNoSecret(t *testing.T) {
	g := New(cfg{}, "", nil)
	if _, err := g.Verify(hook("worker-7", 30000, "", nil)); !errors.Is(err, pay.ErrBadSignature) {
		t.Fatalf("без секрета подпись принята: %v", err)
	}
}

func TestStatus(t *testing.T) {
	ops := []map[string]any{
		{"label": "worker-1", "status": "success", "direction": "in", "amount": 298.5},
		{"label": "worker-2", "status": "refused", "direction": "in", "amount": 300},
		{"label": "worker-3", "status": "success", "direction": "out", "amount": 300},
		{"label": "worker-4", "status": "success", "direction": "in", "amount": 300.0},
	}
	srv := httptest.NewServer(fakeAPI(ops))
	defer srv.Close()
	cases := []struct {
		name   string
		cfg    cfg
		id     int64
		status core.PayStatus
		amount int64
		err    bool
	}{
		{name: "оплачен, amount после комиссии", cfg: full, id: 1, status: core.PayPaid, amount: 29850},
		{name: "оплачен целой суммой", cfg: full, id: 4, status: core.PayPaid, amount: 30000},
		{name: "отказ", cfg: full, id: 2, status: core.PayPending},
		{name: "исходящая операция", cfg: full, id: 3, status: core.PayPending},
		{name: "операции нет", cfg: full, id: 9, status: core.PayPending},
		{name: "без токена ждём", cfg: cfg{"yoomoney.notification_secret": testSecret}, id: 1, status: core.PayPending},
		{name: "токен не принят", cfg: cfg{"yoomoney.token": "bad"}, id: 1, err: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ev, err := New(c.cfg, srv.URL, srv.Client()).Status(t.Context(), core.Payment{ID: c.id})
			if (err != nil) != c.err {
				t.Fatalf("ошибка: %v", err)
			}
			if err != nil && strings.Contains(err.Error(), "bad") {
				t.Fatal("токен попал в ошибку")
			}
			if !c.err && (ev.Status != c.status || ev.ExtAmount != c.amount) {
				t.Fatalf("событие не то: %+v", ev)
			}
		})
	}
}

func TestCreateLink(t *testing.T) {
	g := New(full, "", nil)
	inv, err := g.Create(t.Context(), core.Payment{ID: 42, Amount: 30050})
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(inv.URL)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	if u.Host != "yoomoney.ru" || q.Get("sum") != "300.50" || q.Get("label") != "worker-42" || q.Get("receiver") != "4100111" ||
		inv.ExternalID != "worker-42" || inv.ExtAmount != 30050 || inv.ExtCurrency != "RUB" {
		t.Fatalf("ссылка не та: %+v", inv)
	}
}

func TestKopecks(t *testing.T) {
	for in, want := range map[string]int64{"300": 30000, "300.5": 30050, "298.50": 29850, "0.01": 1, "300.000": 30000} {
		if got, err := kopecks(in); err != nil || got != want {
			t.Errorf("%s: %d %v", in, got, err)
		}
	}
	for _, in := range []string{"", "abc", "-1", "1.005", "0", "0.00"} {
		if _, err := kopecks(in); err == nil {
			t.Errorf("%q принято", in)
		}
	}
}
