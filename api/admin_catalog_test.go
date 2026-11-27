package api

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/bini69-oi/worker/core"
)

const tariffBody = `{"name":"Месяц","traffic_bytes":107374182400,"devices":3,"reset":"MONTH","squads":["sq-1"],"active":true,"sort":1,
"periods":[{"days":30,"price":20000},{"days":90,"price":50000}]}`

func TestAdminTariffsGolden(t *testing.T) {
	e := newAdminEnv(t)
	e.panel.AddSquad("sq-1", "Европа", 12)
	e.panel.AddSquad("sq-2", "Азия", 0)
	rec := e.post(e.owner, "/api/admin/tariffs", tariffBody)
	if rec.Code != 200 {
		t.Fatalf("создание: %d %s", rec.Code, rec.Body)
	}
	rec = e.get(e.owner, "/api/admin/tariffs")
	if rec.Code != 200 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	golden(t, "admin_tariffs", rec.Body.Bytes())
	e.panel.SetDown(true)
	rec = e.get(e.owner, "/api/admin/tariffs")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"squads_ok":false`) || !strings.Contains(rec.Body.String(), `"Месяц"`) {
		t.Errorf("панель лежит, страница должна работать: %d %s", rec.Code, rec.Body)
	}
}

func TestAdminTariffNewPriceAndHide(t *testing.T) {
	ctx := contextBG
	e := newAdminEnv(t)
	u := e.subscriber(2001, "ivan") // купил «Месяц» за 200 ₽
	tr, _ := e.c.AllTariffs(ctx, adminOwner)
	id := tr[0].ID
	path := fmt.Sprintf("/api/admin/tariffs/%d", id)
	rec := e.post(e.owner, path, strings.Replace(tariffBody, `"price":20000`, `"price":25000`, 1))
	if rec.Code != 200 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	sub, _ := e.c.Sub(ctx, u.ID)
	if sub.Price != 20000 {
		t.Errorf("цена купленной подписки изменилась: %d", sub.Price)
	}
	q, err := e.c.Quote(ctx, u.ID, id, 30)
	if err != nil || q.Price != 25000 {
		t.Errorf("новая цена: %+v %v", q, err)
	}
	// Скрытый тариф не виден новым абонентам, но в списке админа остаётся.
	rec = e.post(e.owner, path, strings.Replace(tariffBody, `"active":true`, `"active":false`, 1))
	if rec.Code != 200 {
		t.Fatal(rec.Body)
	}
	if pub, _ := e.c.Tariffs(ctx); len(pub) != 0 {
		t.Errorf("скрытый тариф виден: %v", pub)
	}
	var list AdminTariffList
	if err := json.Unmarshal(e.get(e.owner, "/api/admin/tariffs").Body.Bytes(), &list); err != nil || len(list.Tariffs) != 1 || list.Tariffs[0].Active {
		t.Errorf("список админа: %+v %v", list, err)
	}
	if rec = e.post(e.owner, path, `{"name":"","periods":[]}`); rec.Code != 400 {
		t.Errorf("пустой тариф: %d", rec.Code)
	}
}

func TestAdminPromosGolden(t *testing.T) {
	e := newAdminEnv(t)
	rec := e.post(e.owner, "/api/admin/promos", `{"code":"spring26","kind":"days","value":7,"max_uses":100,"tariff_id":0}`)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"code":"SPRING26"`) {
		t.Fatalf("создание: %d %s", rec.Code, rec.Body)
	}
	rec = e.get(e.owner, "/api/admin/promos")
	golden(t, "admin_promos", rec.Body.Bytes())
	var list AdminPromoList
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil || len(list.Items) != 1 {
		t.Fatal(err, rec.Body)
	}
	if want := "https://t.me/test_bot?start=promo_SPRING26"; list.Items[0].Link != want {
		t.Errorf("ссылка %q, ждали %q", list.Items[0].Link, want)
	}
	id := list.Items[0].ID
	rec = e.post(e.owner, fmt.Sprintf("/api/admin/promos/%d/active", id), `{"active":false}`)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"active":false`) {
		t.Errorf("выключение: %d %s", rec.Code, rec.Body)
	}
}

func TestAdminPromoShortCode(t *testing.T) {
	e := newAdminEnv(t)
	rec := e.post(e.owner, "/api/admin/promos", `{"code":"ABC","kind":"days","value":7}`)
	if rec.Code != 400 || errCode(t, rec) != core.ErrInvalid.Code {
		t.Fatalf("короткий код: %d %s", rec.Code, rec.Body)
	}
	if list := e.get(e.owner, "/api/admin/promos"); !strings.Contains(list.Body.String(), `"items":[]`) {
		t.Errorf("код создан: %s", list.Body)
	}
}
