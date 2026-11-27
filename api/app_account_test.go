package api

import (
	"strings"
	"testing"
	"time"

	"github.com/bini69-oi/worker/core"
)

func TestHistory(t *testing.T) {
	e := newAppEnv(t)
	tok := e.login(41)
	var out HistoryResponse
	decode(t, e.as(tok, "GET", "/api/app/history", ""), &out)
	if out.Items == nil || len(out.Items) != 0 {
		t.Fatalf("пустая история: %+v", out)
	}
	e.pay(41, 30000)
	e.clock.Add(time.Hour)
	e.bought(41, e.tariff("Месяц", 25000, 3, 100))
	rec := e.as(tok, "GET", "/api/app/history", "")
	if rec.Code != 200 {
		t.Fatalf("история: %d", rec.Code)
	}
	golden(t, "history", rec.Body.Bytes())
}

func TestHistoryHidesAdminComments(t *testing.T) {
	e := newAppEnv(t)
	tok := e.login(41)
	u, _ := e.c.UserByTg(t.Context(), 41)
	if _, err := e.c.AdminAdjust(t.Context(), testOwner, u.ID, 500, "подозрение на мошенника"); err != nil {
		t.Fatal(err)
	}
	if body := e.as(tok, "GET", "/api/app/history", "").Body.String(); strings.Contains(body, "подозрение") {
		t.Fatalf("комментарий админа виден абоненту: %s", body)
	}
}

func TestRef(t *testing.T) {
	e := newAppEnv(t)
	tok := e.login(41)
	u, _ := e.c.UserByTg(t.Context(), 41)
	rec := e.as(tok, "GET", "/api/app/ref", "")
	if rec.Code != 200 {
		t.Fatalf("рефералка: %d", rec.Code)
	}
	body := strings.ReplaceAll(rec.Body.String(), u.RefCode, "REFCODE")
	golden(t, "ref", []byte(body))
	var out RefResponse
	decode(t, rec, &out)
	if !strings.HasSuffix(out.Link, "?start=ref_"+u.RefCode) {
		t.Fatalf("ссылка: %s", out.Link)
	}
}

func TestPromo(t *testing.T) {
	e := newAppEnv(t)
	tok := e.login(41)
	if _, err := e.c.SavePromo(t.Context(), testOwner, core.Promo{Code: "BONUS100", Kind: core.PromoMoney, Value: 15000, Active: true}); err != nil {
		t.Fatal(err)
	}
	rec := e.as(tok, "POST", "/api/app/promo", `{"code":"bonus100 "}`)
	if rec.Code != 200 {
		t.Fatalf("промокод: %d %s", rec.Code, rec.Body)
	}
	golden(t, "promo_money", rec.Body.Bytes())
	var bad ErrorBody
	rec = e.as(tok, "POST", "/api/app/promo", `{"code":"BONUS100"}`)
	decode(t, rec, &bad)
	if rec.Code != 409 || bad.Code != "E5002" {
		t.Fatalf("второй раз: %d %+v", rec.Code, bad)
	}
	rec = e.as(tok, "POST", "/api/app/promo", `{"code":"NOPE1234"}`)
	decode(t, rec, &bad)
	if bad.Code != "E5001" {
		t.Fatalf("нет такого: %+v", bad)
	}
	if rec := e.as(tok, "POST", "/api/app/promo", `{"code":""}`); rec.Code != 409 && rec.Code != 400 {
		t.Fatalf("пустой код: %d", rec.Code)
	}
	// Перебор: после десяти неудачных попыток отказ даже на верный код.
	for range 12 {
		e.as(tok, "POST", "/api/app/promo", `{"code":"GUESS123"}`)
		e.clock.Add(time.Second)
	}
	if rec := e.as(tok, "POST", "/api/app/promo", `{"code":"BONUS100"}`); rec.Code != 429 {
		t.Fatalf("после перебора: %d", rec.Code)
	}
}

func TestAutoRenew(t *testing.T) {
	e := newAppEnv(t)
	tok := e.login(41)
	if rec := e.as(tok, "POST", "/api/app/autorenew", `{"on":true}`); rec.Code == 200 {
		t.Fatal("автопродление без подписки включилось")
	}
	e.bought(41, e.tariff("Месяц", 25000, 3, 100))
	rec := e.as(tok, "POST", "/api/app/autorenew", `{"on":true}`)
	if rec.Code != 200 {
		t.Fatalf("включить: %d %s", rec.Code, rec.Body)
	}
	var sub SubView
	decode(t, rec, &sub)
	if !sub.AutoRenew {
		t.Fatalf("автопродление не включено: %+v", sub)
	}
	decode(t, e.as(tok, "POST", "/api/app/autorenew", `{"on":false}`), &sub)
	if sub.AutoRenew {
		t.Fatal("автопродление не выключилось")
	}
	if rec := e.as(tok, "POST", "/api/app/autorenew", `{}`); rec.Code != 400 {
		t.Fatalf("без поля on: %d", rec.Code)
	}
}
