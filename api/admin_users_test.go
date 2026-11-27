package api

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/bini69-oi/worker/core"
)

func TestAdminUsersListGolden(t *testing.T) {
	e := newAdminEnv(t)
	e.subscriber(2001, "ivan")
	if _, err := e.c.Register(contextBG, 2002, "petr", "ru"); err != nil {
		t.Fatal(err)
	}
	rec := e.get(e.owner, "/api/admin/users")
	if rec.Code != 200 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	golden(t, "admin_users_list", rec.Body.Bytes())

	rec = e.get(e.owner, "/api/admin/users?q=@iv&filter=active")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"ivan"`) || strings.Contains(rec.Body.String(), `"petr"`) {
		t.Errorf("поиск: %s", rec.Body)
	}
	rec = e.get(e.owner, "/api/admin/users?q=@zz")
	if !strings.Contains(rec.Body.String(), `"items":[]`) {
		t.Errorf("пустой список должен быть [], а не null: %s", rec.Body)
	}
	if rec = e.get(e.owner, "/api/admin/users?filter=чужой"); rec.Code != 400 {
		t.Errorf("неверный фильтр: %d", rec.Code)
	}
	if rec = e.get(e.owner, "/api/admin/users?cursor=abc"); rec.Code != 400 {
		t.Errorf("неверный курсор: %d", rec.Code)
	}
}

func TestAdminUserCardGolden(t *testing.T) {
	e := newAdminEnv(t)
	u := e.subscriber(2001, "ivan")
	rec := e.get(e.owner, fmt.Sprintf("/api/admin/users/%d", u.ID))
	if rec.Code != 200 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	golden(t, "admin_user_card", rec.Body.Bytes())
	for _, leak := range []string{"sub_url", "panel_uuid", "ref_code", "pass"} {
		if strings.Contains(rec.Body.String(), leak) {
			t.Errorf("в карточке лишнее поле %q", leak)
		}
	}
	e.panel.SetDown(true)
	rec = e.get(e.owner, fmt.Sprintf("/api/admin/users/%d", u.ID))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"devices_ok":false`) {
		t.Errorf("панель лежит: %d %s", rec.Code, rec.Body)
	}
	if rec = e.get(e.owner, "/api/admin/users/9999"); rec.Code != 404 {
		t.Errorf("нет абонента: %d", rec.Code)
	}
	if rec = e.get(e.owner, "/api/admin/users/abc"); rec.Code != 400 {
		t.Errorf("плохой id: %d", rec.Code)
	}
}

func TestAdminUserActions(t *testing.T) {
	e := newAdminEnv(t)
	u := e.subscriber(2001, "ivan")
	base := fmt.Sprintf("/api/admin/users/%d", u.ID)
	before, _ := e.c.User(contextBG, u.ID)

	rec := e.post(e.owner, base+"/balance", `{"amount":10000,"comment":"компенсация"}`)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), fmt.Sprintf(`"balance":%d`, before.Balance+10000)) {
		t.Fatalf("начисление: %d %s", rec.Code, rec.Body)
	}
	rec = e.post(e.owner, base+"/balance", `{"amount":10000,"comment":""}`)
	if rec.Code != 400 || errCode(t, rec) != core.ErrNeedComment.Code {
		t.Errorf("без комментария: %d %s", rec.Code, rec.Body)
	}
	if after, _ := e.c.User(contextBG, u.ID); after.Balance != before.Balance+10000 {
		t.Errorf("баланс изменился без комментария: %d", after.Balance)
	}
	rec = e.post(e.owner, base+"/balance", `{"amount":-99999999,"comment":"много"}`)
	if rec.Code != 402 {
		t.Errorf("списание ниже нуля: %d", rec.Code)
	}

	subBefore, _ := e.c.Sub(contextBG, u.ID)
	rec = e.post(e.owner, base+"/days", `{"days":30}`)
	subAfter, _ := e.c.Sub(contextBG, u.ID)
	if rec.Code != 200 || subAfter.ExpiresAt.Sub(subBefore.ExpiresAt) != 30*24*60*60*1e9 {
		t.Errorf("дни: %d %s", rec.Code, rec.Body)
	}
	if rec = e.post(e.owner, base+"/days", `{"days":0}`); rec.Code != 400 {
		t.Errorf("ноль дней: %d", rec.Code)
	}

	if rec = e.post(e.owner, base+"/ban", `{"comment":"спам"}`); rec.Code != 200 {
		t.Errorf("бан: %d %s", rec.Code, rec.Body)
	}
	if b, _ := e.c.User(contextBG, u.ID); b.BannedAt.IsZero() {
		t.Errorf("не забанен")
	}
	if rec = e.post(e.owner, base+"/unban", `{"comment":"ошибка"}`); rec.Code != 200 {
		t.Errorf("разбан: %d", rec.Code)
	}

	if rec = e.post(e.owner, base+"/message", `{"text":"Проверили, всё работает"}`); rec.Code != 200 {
		t.Errorf("сообщение: %d %s", rec.Code, rec.Body)
	}
	if rec = e.post(e.owner, base+"/devices/reset", `{}`); rec.Code != 200 {
		t.Errorf("сброс устройств: %d %s", rec.Code, rec.Body)
	}
	if rec = e.post(e.owner, base+"/link/revoke", `{}`); rec.Code != 200 {
		t.Errorf("перевыпуск ссылки: %d %s", rec.Code, rec.Body)
	}
	if rec = e.post(e.owner, base+"/balance", `{"amount":1,"comment":"x","лишнее":1}`); rec.Code != 400 {
		t.Errorf("неизвестное поле тела: %d", rec.Code)
	}
}

// Форма с чужого сайта несёт cookie, но не X-Worker: изменение отклоняется.
func TestAdminUserActionNeedsOrigin(t *testing.T) {
	e := newAdminEnv(t)
	u := e.subscriber(2001, "ivan")
	before, _ := e.c.User(contextBG, u.ID)
	req := httpReq(e, http.MethodPost, fmt.Sprintf("/api/admin/users/%d/balance", u.ID), `amount=1000&comment=x`, e.owner)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := serve(e, req)
	if rec.Code != 403 {
		t.Fatalf("чужая форма: %d %s", rec.Code, rec.Body)
	}
	if after, _ := e.c.User(contextBG, u.ID); after.Balance != before.Balance {
		t.Errorf("баланс изменился")
	}
}
