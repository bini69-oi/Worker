package api

import (
	"strings"
	"testing"
	"time"

	"github.com/bini69-oi/worker/core"
)

// ref отдаёт идентификатор абонента в панели.
func (e *appEnv) ref(tgID int64) string {
	e.t.Helper()
	u, _ := e.c.UserByTg(e.t.Context(), tgID)
	s, err := e.c.Sub(e.t.Context(), u.ID)
	if err != nil || s.PanelUUID == "" {
		e.t.Fatalf("абонента нет в панели: %+v %v", s, err)
	}
	return s.PanelUUID
}

func TestConnect(t *testing.T) {
	e := newAppEnv(t)
	if err := e.cfg.Set("connect.apps", "happ,v2raytun"); err != nil {
		t.Fatal(err)
	}
	tok := e.login(51)
	if rec := e.as(tok, "GET", "/api/app/connect", ""); rec.Code != 404 {
		t.Fatalf("без подписки: %d", rec.Code)
	}
	e.bought(51, e.tariff("Месяц", 25000, 3, 100))
	rec := e.as(tok, "GET", "/api/app/connect", "")
	if rec.Code != 200 {
		t.Fatalf("подключение: %d %s", rec.Code, rec.Body)
	}
	// Токен перехода меняется от времени и ключа: в эталоне он заменён.
	var out ConnectResponse
	decode(t, rec, &out)
	body := rec.Body.String()
	for _, a := range out.Apps {
		if !strings.HasPrefix(a.OpenURL, "https://w.test/dl/"+a.ID+"/") {
			t.Fatalf("переход: %s", a.OpenURL)
		}
		body = strings.ReplaceAll(body, a.OpenURL, "https://w.test/dl/"+a.ID+"/TOKEN")
	}
	golden(t, "connect", []byte(body))
}

func TestDevices(t *testing.T) {
	e := newAppEnv(t)
	a, b := e.login(51), e.login(52)
	tr := e.tariff("Месяц", 25000, 3, 100)
	e.bought(51, tr)
	e.bought(52, tr)
	e.panel.AddFullDevice(e.ref(51), core.PanelDevice{HWID: "hw-a1", Platform: "iOS", Model: "iPhone", CreatedAt: t0})
	e.panel.AddFullDevice(e.ref(51), core.PanelDevice{HWID: "hw-a2", Platform: "Android", Model: "Pixel", CreatedAt: t0.Add(time.Hour)})
	e.panel.AddFullDevice(e.ref(52), core.PanelDevice{HWID: "hw-b1", Platform: "Windows", Model: "PC", CreatedAt: t0})

	rec := e.as(a, "GET", "/api/app/devices", "")
	if rec.Code != 200 {
		t.Fatalf("устройства: %d %s", rec.Code, rec.Body)
	}
	golden(t, "devices", rec.Body.Bytes())
	if strings.Contains(rec.Body.String(), "hw-b1") {
		t.Fatal("в списке чужое устройство")
	}

	// Чужое устройство удалить нельзя: у другого абонента оно остаётся.
	e.as(a, "DELETE", "/api/app/devices/hw-b1", "")
	var out DevicesResponse
	decode(t, e.as(b, "GET", "/api/app/devices", ""), &out)
	if len(out.Devices) != 1 {
		t.Fatalf("чужое устройство удалилось: %+v", out)
	}

	if rec := e.as(a, "DELETE", "/api/app/devices/hw-a1", ""); rec.Code != 200 {
		t.Fatalf("удаление: %d %s", rec.Code, rec.Body)
	}
	decode(t, e.as(a, "GET", "/api/app/devices", ""), &out)
	if len(out.Devices) != 1 || out.Devices[0].HWID != "hw-a2" {
		t.Fatalf("после удаления: %+v", out)
	}
	if rec := e.as(a, "POST", "/api/app/devices/reset", "{}"); rec.Code != 200 {
		t.Fatalf("сброс: %d", rec.Code)
	}
	decode(t, e.as(a, "GET", "/api/app/devices", ""), &out)
	if out.Devices == nil || len(out.Devices) != 0 {
		t.Fatalf("после сброса: %+v", out)
	}
	decode(t, e.as(b, "GET", "/api/app/devices", ""), &out)
	if len(out.Devices) != 1 {
		t.Fatal("сброс задел чужие устройства")
	}
}

func TestDevicesPanelDownAndNoSub(t *testing.T) {
	e := newAppEnv(t)
	tok := e.login(51)
	if rec := e.as(tok, "GET", "/api/app/devices", ""); rec.Code != 404 {
		t.Fatalf("без подписки: %d", rec.Code)
	}
	e.bought(51, e.tariff("Месяц", 25000, 3, 100))
	e.panel.SetDown(true)
	rec := e.as(tok, "GET", "/api/app/devices", "")
	var body ErrorBody
	decode(t, rec, &body)
	if rec.Code != 503 || body.Code != "E3008" || strings.Contains(rec.Body.String(), "недоступна") {
		t.Fatalf("панель лежит: %d %s", rec.Code, rec.Body)
	}
	if rec := e.as(tok, "DELETE", "/api/app/devices/"+strings.Repeat("x", 300), ""); rec.Code != 400 {
		t.Fatalf("длинный hwid: %d", rec.Code)
	}
}

func TestRevokeLink(t *testing.T) {
	e := newAppEnv(t)
	tok := e.login(51)
	e.bought(51, e.tariff("Месяц", 25000, 3, 100))
	var before, after ConnectResponse
	decode(t, e.as(tok, "GET", "/api/app/connect", ""), &before)
	rec := e.as(tok, "POST", "/api/app/link/revoke", "{}")
	if rec.Code != 200 {
		t.Fatalf("перевыпуск: %d %s", rec.Code, rec.Body)
	}
	decode(t, rec, &after)
	if after.SubURL == "" || after.SubURL == before.SubURL {
		t.Fatalf("ссылка не сменилась: %q -> %q", before.SubURL, after.SubURL)
	}
}
