package api

import (
	"testing"
	"time"
)

func TestBannedGets403(t *testing.T) {
	e := newAppEnv(t)
	tok := e.login(61)
	u, _ := e.c.UserByTg(t.Context(), 61)
	if err := e.c.Ban(t.Context(), testOwner, u.ID, ""); err != nil {
		t.Fatal(err)
	}
	for _, c := range [][2]string{
		{"GET", "/api/app/me"}, {"GET", "/api/app/tariffs"}, {"GET", "/api/app/history"}, {"GET", "/api/app/texts"},
		{"POST", "/api/app/trial"}, {"POST", "/api/app/logout"}, {"GET", "/api/app/devices"},
	} {
		body := ""
		if c[0] == "POST" {
			body = "{}"
		}
		rec := e.as(tok, c[0], c[1], body)
		if c[1] == "/api/app/texts" { // тексты нужны и странице блокировки: они отдаются без проверки абонента
			if rec.Code != 200 {
				t.Errorf("%s %s: %d", c[0], c[1], rec.Code)
			}
			continue
		}
		var out ErrorBody
		decode(t, rec, &out)
		if rec.Code != 403 || out.Code != "E3006" {
			t.Errorf("%s %s: %d %+v", c[0], c[1], rec.Code, out)
		}
	}
	// Войти заново в Mini App забаненный тоже не может.
	body := tgBody(initData(61, "ru", e.clock.Now()))
	if rec := e.req("POST", "/api/app/session/tg", body); rec.Code != 403 {
		t.Fatalf("вход забаненного: %d", rec.Code)
	}
}

func TestMaintenance(t *testing.T) {
	e := newAppEnv(t)
	e.enable("fake")
	tok := e.login(62)
	tr := e.tariff("Месяц", 25000, 3, 100)
	e.bought(62, tr)
	var pay TopupResponse
	decode(t, e.as(tok, "POST", "/api/app/topup", `{"gateway":"fake","amount":30000}`), &pay)
	if err := e.cfg.Set("maintenance.on", "true"); err != nil {
		t.Fatal(err)
	}
	for _, c := range [][3]string{
		{"POST", "/api/app/trial", "{}"}, {"GET", "/api/app/tariffs", ""}, {"POST", "/api/app/quote", `{"tariff_id":1,"days":30}`},
		{"POST", "/api/app/buy", `{"tariff_id":1,"days":30}`}, {"POST", "/api/app/topup", `{"gateway":"fake","amount":30000}`},
		{"GET", "/api/app/history", ""}, {"GET", "/api/app/ref", ""}, {"POST", "/api/app/promo", `{"code":"ABCDEF"}`},
		{"POST", "/api/app/autorenew", `{"on":true}`}, {"GET", "/api/app/connect", ""}, {"GET", "/api/app/devices", ""},
		{"DELETE", "/api/app/devices/x", ""}, {"POST", "/api/app/devices/reset", "{}"}, {"POST", "/api/app/link/revoke", "{}"},
	} {
		e.clock.Add(200 * time.Millisecond) // общий предел запросов в секунду тут не проверяется
		rec := e.as(tok, c[0], c[1], c[2])
		var out ErrorBody
		decode(t, rec, &out)
		if rec.Code != 503 || out.Code != "E1005" {
			t.Errorf("%s %s во время техработ: %d %+v", c[0], c[1], rec.Code, out)
		}
	}
	// Главная, тексты, выход и статус уже созданного платежа работают.
	for _, c := range [][2]string{{"GET", "/api/app/me"}, {"GET", "/api/app/texts"}, {"GET", "/api/app/payments/" + itoa(pay.PaymentID)}} {
		e.clock.Add(200 * time.Millisecond)
		if rec := e.as(tok, c[0], c[1], ""); rec.Code != 200 {
			t.Errorf("%s %s во время техработ: %d", c[0], c[1], rec.Code)
		}
	}
	e.clock.Add(time.Second)
	if rec := e.as(tok, "POST", "/api/app/logout", "{}"); rec.Code != 200 {
		t.Errorf("выход во время техработ: %d", rec.Code)
	}
	// Админ пользуется кабинетом, пока абоненты ждут: так он проверяет, что всё работает.
	e.login(testOwner)
	owner := e.login(testOwner)
	if rec := e.as(owner, "GET", "/api/app/tariffs", ""); rec.Code != 200 {
		t.Errorf("админ во время техработ: %d", rec.Code)
	}
}
