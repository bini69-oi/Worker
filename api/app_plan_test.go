package api

import (
	"sync"
	"testing"
	"time"
)

func TestPlanQueuedAndCancel(t *testing.T) {
	e := newAppEnv(t)
	tok := e.login(71)
	month := e.tariff("Месяц", 25000, 3, 100)
	year := e.tariff("Год", 200000, 5, 0)
	e.bought(71, month)
	e.topup(71, 200000)
	before, _ := e.c.UserByTg(t.Context(), 71)
	rec := e.as(tok, "POST", "/api/app/buy", `{"tariff_id":`+itoa(year.ID)+`,"days":30}`)
	var bought BuyResponse
	decode(t, rec, &bought)
	if rec.Code != 200 || !bought.Queued || bought.Sub.Next == nil || bought.Sub.Next.Tariff != "Год" {
		t.Fatalf("смена тарифа: %d %s", rec.Code, rec.Body)
	}
	golden(t, "me_plan_queued", e.as(tok, "GET", "/api/app/me", "").Body.Bytes())

	// Двойной клик по «Отменить»: деньги возвращаются один раз.
	e.clock.Add(time.Second)
	codes := make(chan int, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Go(func() { codes <- e.as(tok, "POST", "/api/app/plan/cancel", "{}").Code })
	}
	wg.Wait()
	close(codes)
	ok, notFound := 0, 0
	for c := range codes {
		switch c {
		case 200:
			ok++
		case 404:
			notFound++
		}
	}
	if ok != 1 || notFound != 1 {
		t.Fatalf("двойная отмена: успешных %d, «нет смены» %d", ok, notFound)
	}
	after, _ := e.c.UserByTg(t.Context(), 71)
	if after.Balance != before.Balance {
		t.Fatalf("баланс после отмены %d, был %d", after.Balance, before.Balance)
	}
	var me MeResponse
	decode(t, e.as(tok, "GET", "/api/app/me", ""), &me)
	if me.Sub.Next != nil {
		t.Fatalf("смена осталась: %+v", me.Sub.Next)
	}
}

func TestAddonsOffer(t *testing.T) {
	e := newAppEnv(t)
	if err := e.cfg.Set("addons.traffic", "10:15000,50:50000"); err != nil {
		t.Fatal(err)
	}
	if err := e.cfg.Set("addons.device_month", "10000"); err != nil {
		t.Fatal(err)
	}
	tok := e.login(72)
	e.bought(72, e.tariff("Месяц", 25000, 3, 100))
	rec := e.as(tok, "GET", "/api/app/me", "")
	var me MeResponse
	decode(t, rec, &me)
	if len(me.Addons.Traffic) != 2 || me.Addons.DevicePrice != 10000 {
		t.Fatalf("предложение докупки: %+v", me.Addons)
	}
	golden(t, "me_addons", rec.Body.Bytes())
}

func TestAddonsBuy(t *testing.T) {
	e := newAppEnv(t)
	if err := e.cfg.Set("addons.traffic", "10:15000"); err != nil {
		t.Fatal(err)
	}
	if err := e.cfg.Set("addons.device_month", "10000"); err != nil {
		t.Fatal(err)
	}
	tok := e.login(72)
	e.bought(72, e.tariff("Месяц", 25000, 3, 100))
	u, _ := e.c.UserByTg(t.Context(), 72)

	rec := e.as(tok, "POST", "/api/app/addons/traffic", `{"gb":10}`)
	var sub SubView
	decode(t, rec, &sub)
	if rec.Code != 200 || sub.ExtraTrafficBytes != 10<<30 || sub.TrafficBytes != 110<<30 {
		t.Fatalf("докупка трафика: %d %s", rec.Code, rec.Body)
	}
	if rec := e.as(tok, "POST", "/api/app/addons/traffic", `{"gb":7}`); rec.Code != 409 {
		t.Fatalf("пакета нет в настройке: %d", rec.Code)
	}
	rec = e.as(tok, "POST", "/api/app/addons/device", "{}")
	decode(t, rec, &sub)
	if rec.Code != 200 || sub.ExtraDevices != 1 || sub.Devices != 4 {
		t.Fatalf("докупка устройства: %d %s", rec.Code, rec.Body)
	}

	// Денег хватает на одно устройство: при двойном клике второе не покупается и баланс не уходит в минус.
	cur, _ := e.c.UserByTg(t.Context(), 72)
	if _, err := e.c.AdminAdjust(t.Context(), testOwner, u.ID, 10000-cur.Balance, "точная сумма"); err != nil {
		t.Fatal(err)
	}
	e.clock.Add(time.Second)
	codes := make(chan int, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Go(func() { codes <- e.as(tok, "POST", "/api/app/addons/device", "{}").Code })
	}
	wg.Wait()
	close(codes)
	ok, need := 0, 0
	for c := range codes {
		switch c {
		case 200:
			ok++
		case 402:
			need++
		}
	}
	if ok != 1 || need != 1 {
		t.Fatalf("двойной клик: успешных %d, «не хватает» %d", ok, need)
	}
	if end, _ := e.c.UserByTg(t.Context(), 72); end.Balance < 0 || end.Balance > 10000 {
		t.Fatalf("баланс: %d", end.Balance)
	}
}

func TestAddonsNeedMoneyAndTrial(t *testing.T) {
	e := newAppEnv(t)
	if err := e.cfg.Set("addons.traffic", "10:15000"); err != nil {
		t.Fatal(err)
	}
	if err := e.cfg.Set("trial.enabled", "true"); err != nil {
		t.Fatal(err)
	}
	tok := e.login(73)
	if rec := e.as(tok, "POST", "/api/app/trial", "{}"); rec.Code != 200 {
		t.Fatal(rec.Body)
	}
	if rec := e.as(tok, "POST", "/api/app/addons/traffic", `{"gb":10}`); rec.Code != 409 {
		t.Fatalf("докупка к пробной: %d", rec.Code)
	}
	if rec := e.as(tok, "POST", "/api/app/plan/cancel", "{}"); rec.Code != 404 {
		t.Fatalf("отмена без смены: %d", rec.Code)
	}
	e2 := e.login(74)
	e.bought(74, e.tariff("Месяц", 25000, 3, 100))
	u, _ := e.c.UserByTg(t.Context(), 74)
	if _, err := e.c.AdminAdjust(t.Context(), testOwner, u.ID, -(75000), "обнулить"); err != nil {
		t.Fatal(err)
	}
	var body ErrorBody
	rec := e.as(e2, "POST", "/api/app/addons/traffic", `{"gb":10}`)
	decode(t, rec, &body)
	if rec.Code != 402 || body.Money == nil || body.Money.Price != 15000 || body.Money.Missing != 15000 {
		t.Fatalf("нет денег на докупку: %d %+v", rec.Code, body)
	}
}
