package api

import (
	"testing"
	"time"

	"github.com/bini69-oi/worker/core"
)

// tariff заводит тариф с периодом на 30 дней.
func (e *appEnv) tariff(name string, price int64, devices int, trafficGB int64) core.Tariff {
	e.t.Helper()
	tr, err := e.c.SaveTariff(e.t.Context(), testOwner, core.Tariff{Name: name, Active: true, Devices: devices,
		TrafficBytes: trafficGB << 30, Reset: "MONTH", Squads: []string{"s1"},
		Periods: []core.Period{{Days: 30, Price: price}, {Days: 90, Price: price * 3}}})
	if err != nil {
		e.t.Fatal(err)
	}
	return tr
}

// topup кладёт деньги на баланс абонента tgID, как это делает админ.
func (e *appEnv) topup(tgID, amount int64) {
	e.t.Helper()
	u, err := e.c.UserByTg(e.t.Context(), tgID)
	if err != nil {
		e.t.Fatal(err)
	}
	if _, err := e.c.AdminAdjust(e.t.Context(), testOwner, u.ID, amount, "тест"); err != nil {
		e.t.Fatal(err)
	}
}

// bought покупает тариф на 30 дней и прогоняет очередь, чтобы абонент появился в панели.
func (e *appEnv) bought(tgID int64, tr core.Tariff) {
	e.t.Helper()
	e.topup(tgID, 100000)
	u, _ := e.c.UserByTg(e.t.Context(), tgID)
	if _, err := e.c.Buy(e.t.Context(), u.ID, tr.ID, 30); err != nil {
		e.t.Fatal(err)
	}
	e.process()
}

func (e *appEnv) process() {
	e.t.Helper()
	if _, err := e.c.ProcessDue(e.t.Context()); err != nil {
		e.t.Fatal(err)
	}
}

func TestMeWithSub(t *testing.T) {
	e := newAppEnv(t)
	e.login(11)
	e.bought(11, e.tariff("Месяц", 25000, 3, 100))
	e.clock.Add(2*24*time.Hour + time.Hour)
	tok := e.login(11) // сессия Telegram живёт сутки
	rec := e.as(tok, "GET", "/api/app/me", "")
	if rec.Code != 200 {
		t.Fatalf("me: %d %s", rec.Code, rec.Body)
	}
	golden(t, "me_with_sub", rec.Body.Bytes())
}

func TestMeNoSub(t *testing.T) {
	e := newAppEnv(t)
	if err := e.cfg.Set("trial.enabled", "true"); err != nil {
		t.Fatal(err)
	}
	tok := e.login(11)
	golden(t, "me_no_sub", e.as(tok, "GET", "/api/app/me", "").Body.Bytes())

	// Пробный взят: кнопки пробного больше нет.
	u, _ := e.c.UserByTg(t.Context(), 11)
	if _, err := e.c.Trial(t.Context(), u.ID); err != nil {
		t.Fatal(err)
	}
	var me MeResponse
	decode(t, e.as(tok, "GET", "/api/app/me", ""), &me)
	if me.TrialAvailable || me.Sub == nil || me.Sub.State != "trial" {
		t.Fatalf("после пробного: %+v", me)
	}
}

func TestMeMaintenance(t *testing.T) {
	e := newAppEnv(t)
	tok := e.login(11)
	if err := e.cfg.Set("maintenance.on", "true"); err != nil {
		t.Fatal(err)
	}
	rec := e.as(tok, "GET", "/api/app/me", "")
	if rec.Code != 200 {
		t.Fatalf("главная во время техработ: %d", rec.Code)
	}
	golden(t, "me_maintenance", rec.Body.Bytes())
}

func TestMeExpired(t *testing.T) {
	e := newAppEnv(t)
	e.login(11)
	e.bought(11, e.tariff("Месяц", 25000, 3, 0))
	e.clock.Add(31 * 24 * time.Hour)
	tok := e.login(11)
	var me MeResponse
	decode(t, e.as(tok, "GET", "/api/app/me", ""), &me)
	if me.Sub == nil || me.Sub.State != "expired" || me.Sub.DaysLeft != 0 {
		t.Fatalf("истёкшая: %+v", me.Sub)
	}
}

func TestTextsOpenAndPerLang(t *testing.T) {
	e := newAppEnv(t)
	// Без сессии: тексты нужны уже странице входа. Язык из параметра, потом из Accept-Language.
	var out TextsResponse
	rec := e.req("GET", "/api/app/texts?lang=en", "")
	if rec.Code != 200 {
		t.Fatalf("тексты без сессии: %d", rec.Code)
	}
	golden(t, "texts_en", rec.Body.Bytes())
	decode(t, rec, &out)
	if out.Lang != "en" || out.Texts["app.hello"] != "en:app.hello" || out.Texts["err.x"] != "en:err.x" {
		t.Fatalf("ответ: %+v", out)
	}
	decode(t, e.req("GET", "/api/app/texts", "", "Accept-Language", "de-DE,de;q=0.9"), &out)
	if out.Lang != "en" {
		t.Fatalf("Accept-Language: %+v", out)
	}
	decode(t, e.req("GET", "/api/app/texts", "", "Accept-Language", "ru-RU"), &out)
	if out.Lang != "ru" {
		t.Fatalf("русский Accept-Language: %+v", out)
	}
	// С сессией язык абонента важнее параметра.
	tok := e.login(11)
	decode(t, e.as(tok, "GET", "/api/app/texts?lang=en", ""), &out)
	if out.Lang != "ru" || out.Texts["app.hello"] != "ru:app.hello" {
		t.Fatalf("язык абонента: %+v", out)
	}
	// Мусор в параметре не ломает ответ.
	if rec := e.req("GET", "/api/app/texts?lang=%27%3Bdrop", ""); rec.Code != 200 {
		t.Fatalf("мусорный язык: %d", rec.Code)
	}
}

// pay проводит платёж через ручную кассу: абонент создал счёт, админ подтвердил.
func (e *appEnv) pay(tgID, amount int64) {
	e.t.Helper()
	u, err := e.c.UserByTg(e.t.Context(), tgID)
	if err != nil {
		e.t.Fatal(err)
	}
	p, _, err := e.c.CreatePayment(e.t.Context(), u.ID, "manual", amount)
	if err != nil {
		e.t.Fatal(err)
	}
	if err := e.c.ConfirmManual(e.t.Context(), testOwner, p.ID); err != nil {
		e.t.Fatal(err)
	}
}
