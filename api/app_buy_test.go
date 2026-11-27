package api

import (
	"strconv"
	"testing"
	"time"
)

func TestTrial(t *testing.T) {
	e := newAppEnv(t)
	if err := e.cfg.Set("trial.enabled", "true"); err != nil {
		t.Fatal(err)
	}
	tok := e.login(21)
	rec := e.as(tok, "POST", "/api/app/trial", "{}")
	if rec.Code != 200 {
		t.Fatalf("пробный: %d %s", rec.Code, rec.Body)
	}
	golden(t, "trial", rec.Body.Bytes())
	var bad ErrorBody
	rec = e.as(tok, "POST", "/api/app/trial", "{}")
	decode(t, rec, &bad)
	if rec.Code != 409 || bad.Code != "E3005" {
		t.Fatalf("второй пробный: %d %+v", rec.Code, bad)
	}
}

func TestTariffs(t *testing.T) {
	e := newAppEnv(t)
	tok := e.login(21)
	e.tariff("Месяц", 25000, 3, 100)
	hidden := e.tariff("Старый", 100, 1, 0)
	hidden.Active = false
	if _, err := e.c.SaveTariff(t.Context(), testOwner, hidden); err != nil {
		t.Fatal(err)
	}
	rec := e.as(tok, "GET", "/api/app/tariffs", "")
	if rec.Code != 200 {
		t.Fatalf("тарифы: %d %s", rec.Code, rec.Body)
	}
	golden(t, "tariffs", rec.Body.Bytes())
}

func TestTariffsEmpty(t *testing.T) {
	e := newAppEnv(t)
	tok := e.login(21)
	var out TariffsResponse
	decode(t, e.as(tok, "GET", "/api/app/tariffs", ""), &out)
	if out.Tariffs == nil {
		t.Fatal("пустой список отдан как null")
	}
}

func TestQuote(t *testing.T) {
	e := newAppEnv(t)
	tok := e.login(21)
	tr := e.tariff("Месяц", 25000, 3, 100)
	body := `{"tariff_id":` + strconv.FormatInt(tr.ID, 10) + `,"days":30}`
	rec := e.as(tok, "POST", "/api/app/quote", body)
	if rec.Code != 200 {
		t.Fatalf("расчёт: %d %s", rec.Code, rec.Body)
	}
	golden(t, "quote_short", rec.Body.Bytes())
	e.topup(21, 40000)
	golden(t, "quote_enough", e.as(tok, "POST", "/api/app/quote", body).Body.Bytes())
	if rec := e.as(tok, "POST", "/api/app/quote", `{"tariff_id":999,"days":30}`); rec.Code != 404 {
		t.Fatalf("неизвестный тариф: %d", rec.Code)
	}
	if rec := e.as(tok, "POST", "/api/app/quote", `{"tariff_id":`+strconv.FormatInt(tr.ID, 10)+`,"days":31}`); rec.Code != 409 {
		t.Fatalf("срока нет: %d", rec.Code)
	}
}

func TestBuyWithoutMoney(t *testing.T) {
	e := newAppEnv(t)
	tok := e.login(21)
	tr := e.tariff("Месяц", 25000, 3, 100)
	e.topup(21, 10000)
	rec := e.as(tok, "POST", "/api/app/buy", `{"tariff_id":`+strconv.FormatInt(tr.ID, 10)+`,"days":30}`)
	if rec.Code != 402 {
		t.Fatalf("статус %d", rec.Code)
	}
	golden(t, "buy_need_money", rec.Body.Bytes())
	u, _ := e.c.UserByTg(t.Context(), 21)
	if u.Balance != 10000 {
		t.Fatalf("деньги ушли: %d", u.Balance)
	}
	// Корзина запомнена: после оплаты недостачи подписка выдаётся сама.
	e.pay(21, 15000)
	u, _ = e.c.UserByTg(t.Context(), 21)
	if sub, err := e.c.Sub(t.Context(), u.ID); err != nil || sub.TariffID != tr.ID {
		t.Fatalf("корзина не сработала: %+v %v", sub, err)
	}
}

func TestBuy(t *testing.T) {
	e := newAppEnv(t)
	tok := e.login(21)
	tr := e.tariff("Месяц", 25000, 3, 100)
	e.topup(21, 60000)
	rec := e.as(tok, "POST", "/api/app/buy", `{"tariff_id":`+strconv.FormatInt(tr.ID, 10)+`,"days":30}`)
	if rec.Code != 200 {
		t.Fatalf("покупка: %d %s", rec.Code, rec.Body)
	}
	golden(t, "buy", rec.Body.Bytes())
	// Продление: срок растёт от конца подписки.
	rec = e.as(tok, "POST", "/api/app/buy", `{"tariff_id":`+strconv.FormatInt(tr.ID, 10)+`,"days":30}`)
	var out BuyResponse
	decode(t, rec, &out)
	if rec.Code != 200 || !out.Sub.ExpiresAt.Equal(t0.Add(60*24*time.Hour)) {
		t.Fatalf("продление: %d %+v", rec.Code, out.Sub)
	}
}

func TestBuyBodyChecks(t *testing.T) {
	e := newAppEnv(t)
	tok := e.login(21)
	for name, body := range map[string]string{
		"лишнее поле": `{"tariff_id":1,"days":30,"user_id":5}`,
		"пусто":       `{}`,
		"минус":       `{"tariff_id":1,"days":-5}`,
		"не JSON":     `tariff`,
	} {
		if rec := e.as(tok, "POST", "/api/app/buy", body); rec.Code != 400 {
			t.Errorf("%s: %d", name, rec.Code)
		}
	}
}
