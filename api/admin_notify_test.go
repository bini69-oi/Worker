package api

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestAdminNotifications(t *testing.T) {
	e := newAdminEnv(t)
	rec := e.get(e.owner, "/api/admin/notifications")
	var n AdminNotify
	if err := json.Unmarshal(rec.Body.Bytes(), &n); err != nil || n.Bound || len(n.Topics) != 7 {
		t.Fatalf("до привязки: %+v %s", n, rec.Body)
	}

	rec = e.post(e.owner, "/api/admin/notifications/code", `{}`)
	var code AdminChatCode
	if err := json.Unmarshal(rec.Body.Bytes(), &code); err != nil || len(code.Code) < 8 {
		t.Fatalf("код: %s", rec.Body)
	}
	// Владелец прислал код в группу: бот зовёт BindChat.
	if err := e.c.BindChat(contextBG, -100500, adminOwner, code.Code); err != nil {
		t.Fatal(err)
	}
	rec = e.get(e.owner, "/api/admin/notifications")
	golden(t, "admin_notifications", rec.Body.Bytes())
	if err := json.Unmarshal(rec.Body.Bytes(), &n); err != nil || !n.Bound || len(n.Topics) != 7 {
		t.Fatalf("после привязки: %+v", n)
	}
	for _, tp := range n.Topics {
		if tp.ID == "security" && (!tp.Locked || !tp.Enabled) {
			t.Errorf("тема безопасности: %+v", tp)
		}
		if tp.ID != "security" && tp.Locked {
			t.Errorf("у темы %s не должно быть замка", tp.ID)
		}
	}

	// Тему включают и выключают настройкой, безопасность выключить нельзя.
	if rec = e.post(e.owner, "/api/admin/settings/set", `{"key":"notify.payments","value":"false"}`); rec.Code != 200 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if !strings.Contains(e.get(e.owner, "/api/admin/notifications").Body.String(), `"id":"payments","title":"ru:topic.payments","enabled":false`) {
		t.Errorf("тема не выключилась")
	}
	if rec = e.post(e.owner, "/api/admin/settings/set", `{"key":"notify.security","value":"false"}`); rec.Code != 400 {
		t.Errorf("безопасность выключилась: %d", rec.Code)
	}
}
