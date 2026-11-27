package api

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/bini69-oi/worker/core"
)

func TestAdminBroadcastFlow(t *testing.T) {
	e := newAdminEnv(t)
	for tg := int64(2001); tg <= 2030; tg++ {
		if _, err := e.c.Register(contextBG, tg, "u", "ru"); err != nil {
			t.Fatal(err)
		}
	}
	rec := e.get(e.owner, "/api/admin/broadcasts/segment?segment=all")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"count":30`) {
		t.Fatalf("счётчик: %d %s", rec.Code, rec.Body)
	}
	if rec = e.get(e.owner, "/api/admin/broadcasts/segment?segment=чужие"); rec.Code != 400 {
		t.Errorf("неизвестный сегмент: %d", rec.Code)
	}

	// Чужой тег: не сохранено, админ видит, какой тег не разрешён.
	rec = e.post(e.owner, "/api/admin/broadcasts", `{"segment":"all","text":"<script>alert(1)</script>"}`)
	var eb ErrorBody
	_ = json.Unmarshal(rec.Body.Bytes(), &eb)
	if rec.Code != 400 || eb.Code != core.ErrBadMarkup.Code || !strings.Contains(eb.Detail, "script") {
		t.Fatalf("чужой тег: %d %s", rec.Code, rec.Body)
	}

	rec = e.post(e.owner, "/api/admin/broadcasts", `{"segment":"all","text":"<b>Привет</b>","button_text":"Открыть","button_url":"https://example.com"}`)
	if rec.Code != 200 {
		t.Fatalf("создание: %d %s", rec.Code, rec.Body)
	}
	var b AdminBroadcast
	_ = json.Unmarshal(rec.Body.Bytes(), &b)
	base := fmt.Sprintf("/api/admin/broadcasts/%d", b.ID)

	if rec = e.post(e.owner, base+"/test", `{}`); rec.Code != 200 {
		t.Fatalf("тест себе: %d %s", rec.Code, rec.Body)
	}
	if msgs := e.sender.Messages(); len(msgs) != 1 || msgs[0].TgID != adminOwner {
		t.Fatalf("тест ушёл не только админу: %+v", msgs)
	}
	golden(t, "admin_broadcast_draft", e.get(e.owner, base).Body.Bytes())

	if rec = e.post(e.owner, base+"/start", `{}`); rec.Code != 200 || !strings.Contains(rec.Body.String(), `"pending":true`) {
		t.Fatalf("старт: %d %s", rec.Code, rec.Body)
	}
	if _, err := e.c.RunBroadcast(contextBG); err != nil || len(e.sender.Messages()) != 1 {
		t.Fatalf("без «Да» рассылка пошла: %v %d", err, len(e.sender.Messages()))
	}
	e.confirmLast(true)
	// Вторая рассылка, пока идёт первая.
	rec = e.post(e.owner, "/api/admin/broadcasts", `{"segment":"all","text":"Вторая"}`)
	var second AdminBroadcast
	_ = json.Unmarshal(rec.Body.Bytes(), &second)
	rec = e.post(e.owner, fmt.Sprintf("/api/admin/broadcasts/%d/start", second.ID), `{}`)
	if rec.Code != 409 || errCode(t, rec) != core.ErrBroadcastBusy.Code {
		t.Fatalf("вторая рассылка: %d %s", rec.Code, rec.Body)
	}

	if _, err := e.c.RunBroadcast(contextBG); err != nil {
		t.Fatal(err)
	}
	e.clock.Add(time.Second)
	golden(t, "admin_broadcast_running", e.get(e.owner, base).Body.Bytes())
	if rec = e.post(e.owner, base+"/cancel", `{}`); rec.Code != 200 {
		t.Fatalf("отмена: %d %s", rec.Code, rec.Body)
	}
	rec = e.get(e.owner, "/api/admin/broadcasts")
	if !strings.Contains(rec.Body.String(), `"status":"cancelled"`) || !strings.Contains(rec.Body.String(), `"sent":25`) {
		t.Errorf("список: %s", rec.Body)
	}
	if n := len(e.sender.Messages()); n != 26 {
		t.Errorf("после отмены ушло %d сообщений", n)
	}
}
