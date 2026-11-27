package api

import (
	"strings"
	"testing"

	"github.com/bini69-oi/worker/core"
)

func TestAdminTextsGolden(t *testing.T) {
	e := newAdminEnv(t)
	rec := e.post(e.owner, "/api/admin/texts/set", `{"key":"buy.success","lang":"ru","value":"Готово, ещё {days} дн."}`)
	if rec.Code != 200 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	rec = e.get(e.owner, "/api/admin/texts?q=buy.success")
	golden(t, "admin_texts", rec.Body.Bytes())
	// Опечатка в подстановке: 400, в ответе список допустимых.
	rec = e.post(e.owner, "/api/admin/texts/set", `{"key":"buy.success","lang":"ru","value":"ещё {dayz}"}`)
	if rec.Code != 400 || !strings.Contains(rec.Body.String(), "{days}") || errCode(t, rec) != core.ErrBadText.Code {
		t.Errorf("опечатка: %d %s", rec.Code, rec.Body)
	}
	rec = e.post(e.owner, "/api/admin/texts/reset", `{"key":"buy.success","lang":"ru"}`)
	if rec.Code != 200 || !strings.Contains(e.get(e.owner, "/api/admin/texts?q=buy.success").Body.String(), `"changed_ru":false`) {
		t.Errorf("сброс: %d %s", rec.Code, rec.Body)
	}
}

func TestAdminSettingsGoldenAndMask(t *testing.T) {
	e := newAdminEnv(t)
	rec := e.post(e.owner, "/api/admin/settings/set", `{"key":"trial.days","value":"7"}`)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"pending":false`) {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	golden(t, "admin_settings", e.get(e.owner, "/api/admin/settings?q=trial.days").Body.Bytes())
	if rec = e.post(e.owner, "/api/admin/settings/set", `{"key":"trial.days","value":"много"}`); rec.Code != 400 || errCode(t, rec) != core.ErrBadSetting.Code {
		t.Errorf("неверный тип: %d %s", rec.Code, rec.Body)
	}

	// Секрет: ответ «ждёт Да», потом маска, самого ключа нигде нет.
	const secret = "live_0123456789abcdefXYZ1"
	rec = e.post(e.owner, "/api/admin/settings/set", `{"key":"yookassa.secret_key","value":"`+secret+`"}`)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"pending":true`) {
		t.Fatalf("секрет: %d %s", rec.Code, rec.Body)
	}
	if strings.Contains(e.get(e.owner, "/api/admin/settings?q=yookassa.secret").Body.String(), "XYZ1") {
		t.Fatal("новое значение секрета видно до «Да»")
	}
	e.confirmLast(true)
	for _, path := range []string{"/api/admin/settings", "/api/admin/gateways", "/api/admin/settings?q=yookassa.secret_key"} {
		body := e.get(e.owner, path).Body.String()
		if strings.Contains(body, secret) {
			t.Fatalf("%s: в ответе секрет целиком", path)
		}
	}
	list := e.get(e.owner, "/api/admin/settings?q=yookassa.secret_key").Body.String()
	if !strings.Contains(list, `"value":"••••XYZ1"`) || !strings.Contains(list, `"secret":true`) {
		t.Errorf("маска: %s", list)
	}
}

func TestAdminSettingMoneyNeedsYes(t *testing.T) {
	e := newAdminEnv(t)
	old := e.cfg.String("manual.details")
	rec := e.post(e.owner, "/api/admin/settings/set", `{"key":"manual.details","value":"Карта 5555"}`)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"pending":true`) {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if e.cfg.String("manual.details") != old {
		t.Fatal("сменилось до «Да»")
	}
	e.confirmLast(false)
	if e.cfg.String("manual.details") != old {
		t.Fatal("сменилось после «Нет»")
	}
}

func TestAdminGatewaysGolden(t *testing.T) {
	e := newAdminEnv(t)
	rec := e.get(e.owner, "/api/admin/gateways")
	if rec.Code != 200 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	golden(t, "admin_gateways", rec.Body.Bytes())
	// Включить кассу без ключей нельзя: сначала включённую выключаем и сбрасываем реквизиты.
	if _, err := e.c.ResetSetting(contextBG, adminOwner, "manual.enabled"); err != nil {
		t.Fatal(err)
	}
	e.cfg.Reset("manual.details")
	rec = e.post(e.owner, "/api/admin/settings/set", `{"key":"manual.enabled","value":"true"}`)
	if rec.Code != 409 || errCode(t, rec) != core.ErrNoKeys.Code {
		t.Errorf("касса без ключей: %d %s", rec.Code, rec.Body)
	}
}
