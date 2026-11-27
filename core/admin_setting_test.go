package core_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/bini69-oi/worker/core"
)

// confirmTo отвечает на последнее подтверждение владельцу, не считая входов.
func confirmLast(t *testing.T, e *cfgEnv, yes bool) {
	t.Helper()
	var last int64
	for _, task := range e.st.Tasks() {
		if task.Kind != core.KindNotifyConfirm {
			continue
		}
		var n core.ConfirmTask
		if err := json.Unmarshal(task.Payload, &n); err == nil && n.Kind != "login" {
			last = n.ConfirmID
		}
	}
	if last == 0 {
		t.Fatal("нет подтверждения владельцу")
	}
	if err := e.c.AnswerConfirm(context.Background(), last, owner, yes); err != nil {
		t.Fatal(err)
	}
}

func TestSetSettingImmediately(t *testing.T) {
	ctx := context.Background()
	e := newCfgEnv(t)
	res, err := e.c.SetSetting(ctx, owner, "trial.days", "7")
	if err != nil || res.Pending {
		t.Fatalf("%+v %v", res, err)
	}
	if e.cfg.Int("trial.days") != 7 {
		t.Errorf("настройка не применилась")
	}
	log := e.st.Audit()
	if a := log[len(log)-1]; a.Action != "setting.set" || a.Target != "trial.days" || a.Details["old"] != "3" || a.Details["new"] != "7" {
		t.Errorf("журнал: %+v", a)
	}
	if _, err := e.c.ResetSetting(ctx, owner, "trial.days"); err != nil || e.cfg.Int("trial.days") != 3 {
		t.Errorf("сброс: %v, значение %d", err, e.cfg.Int("trial.days"))
	}
}

func TestSetSettingWrongType(t *testing.T) {
	ctx := context.Background()
	e := newCfgEnv(t)
	_, err := e.c.SetSetting(ctx, owner, "trial.days", "много")
	var d interface{ Detail() string }
	if !errors.Is(err, core.ErrBadSetting) || !errors.As(err, &d) || !strings.Contains(d.Detail(), "int") {
		t.Fatalf("неверный тип: %v", err)
	}
	if e.cfg.Int("trial.days") != 3 {
		t.Errorf("значение изменилось")
	}
	if _, err := e.c.SetSetting(ctx, owner, "нет.такой", "1"); !errors.Is(err, core.ErrNotFound) {
		t.Errorf("неизвестный ключ: %v", err)
	}
	// Денежная с неверным значением отклоняется сразу, подтверждения владельцу нет.
	before := len(e.st.Tasks())
	if _, err := e.c.SetSetting(ctx, owner, "stars.rate", "дорого"); !errors.Is(err, core.ErrBadSetting) || len(e.st.Tasks()) != before {
		t.Errorf("плохая денежная ушла на подтверждение: %v", err)
	}
}

// Реквизиты ручной оплаты меняются только после «Да»: до него старое значение.
func TestSetMoneySettingNeedsYes(t *testing.T) {
	ctx := context.Background()
	e := newCfgEnv(t)
	old := e.cfg.String("manual.details")
	res, err := e.c.SetSetting(ctx, owner, "manual.details", "Карта 1111 2222")
	if err != nil || !res.Pending || res.ConfirmID == 0 {
		t.Fatalf("%+v %v", res, err)
	}
	if e.cfg.String("manual.details") != old {
		t.Fatalf("реквизиты сменились до «Да»")
	}
	confirmLast(t, e, true)
	if got := e.cfg.String("manual.details"); got != "Карта 1111 2222" {
		t.Fatalf("после «Да»: %q", got)
	}
	var applied bool
	for _, a := range e.st.Audit() {
		applied = applied || (a.Action == "setting.set" && a.Target == "manual.details" && a.Details["new"] == "Карта 1111 2222")
	}
	if !applied {
		t.Errorf("смена реквизитов не записана в журнал")
	}
}

// Угнанная сессия владельца: владелец нажал «Нет», реквизиты прежние, в журнале отказ.
func TestSetMoneySettingRejected(t *testing.T) {
	ctx := context.Background()
	e := newCfgEnv(t)
	old := e.cfg.String("manual.details")
	if _, err := e.c.SetSetting(ctx, owner, "manual.details", "Карта чужая"); err != nil {
		t.Fatal(err)
	}
	confirmLast(t, e, false)
	if e.cfg.String("manual.details") != old {
		t.Fatalf("реквизиты сменились после «Нет»")
	}
	rejected := false
	for _, a := range e.st.Audit() {
		rejected = rejected || strings.HasPrefix(a.Action, "reject:")
	}
	if !rejected {
		t.Errorf("отказа нет в журнале")
	}
}

func TestSetSecretSetting(t *testing.T) {
	ctx := context.Background()
	e := newCfgEnv(t)
	const secret = "live_0123456789abcdefXYZ1"
	res, err := e.c.SetSetting(ctx, owner, "yookassa.secret_key", secret)
	if err != nil || !res.Pending {
		t.Fatalf("%+v %v", res, err)
	}
	if e.cfg.Secret("yookassa.secret_key") != "" {
		t.Fatal("секрет сменился до «Да»")
	}
	confirmLast(t, e, true)
	if e.cfg.Secret("yookassa.secret_key") != secret {
		t.Fatal("секрет не сменился после «Да»")
	}
	// Секрета нет ни в журнале, ни в очереди уведомлений, ни в списке настроек.
	for _, a := range e.st.Audit() {
		for _, v := range a.Details {
			if strings.Contains(v, secret) {
				t.Errorf("секрет в журнале: %+v", a)
			}
		}
	}
	for _, task := range e.st.Tasks() {
		if strings.Contains(string(task.Payload), secret) {
			t.Errorf("секрет в очереди: %s", task.Kind)
		}
	}
	list, err := e.c.SettingList(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range list {
		if strings.Contains(s.Value, secret) || strings.Contains(s.Default, secret) {
			t.Errorf("секрет в списке: %+v", s)
		}
		if s.Key == "yookassa.secret_key" && (s.Value != "••••XYZ1" || !s.Secret || !s.Changed || !s.Confirm) {
			t.Errorf("маска: %+v", s)
		}
	}
}

func TestSettingHelperDenied(t *testing.T) {
	ctx := context.Background()
	e := newCfgEnv(t)
	helperWith(t, e.env, 50, core.PermUsers, core.PermGrant)
	if _, err := e.c.SetSetting(ctx, 50, "trial.days", "30"); !errors.Is(err, core.ErrOwnerOnly) {
		t.Errorf("set: %v", err)
	}
	if _, err := e.c.ResetSetting(ctx, 50, "trial.days"); !errors.Is(err, core.ErrOwnerOnly) {
		t.Errorf("reset: %v", err)
	}
	if _, err := e.c.SettingList(ctx, 50); !errors.Is(err, core.ErrOwnerOnly) {
		t.Errorf("list: %v", err)
	}
	if e.cfg.Int("trial.days") != 3 {
		t.Errorf("помощник изменил настройку")
	}
}

func TestSettingListShowsChanged(t *testing.T) {
	ctx := context.Background()
	e := newCfgEnv(t)
	if _, err := e.c.SetSetting(ctx, owner, "trial.days", "9"); err != nil {
		t.Fatal(err)
	}
	list, err := e.c.SettingList(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	seen := 0
	for _, s := range list {
		switch s.Key {
		case "trial.days":
			seen++
			if s.Value != "9" || s.Default != "3" || !s.Changed || s.Confirm || s.Group != "trial" || s.Kind != "int" {
				t.Errorf("%+v", s)
			}
		case "manual.details":
			seen++
			if !s.Confirm || s.Changed {
				t.Errorf("%+v", s)
			}
		}
	}
	if seen != 2 {
		t.Errorf("не нашли настройки в списке")
	}
}

func TestSecurityTopicCannotBeOff(t *testing.T) {
	ctx := context.Background()
	e := newCfgEnv(t)
	for _, v := range []string{"false", "0", ""} {
		if _, err := e.c.SetSetting(ctx, owner, "notify.security", v); !errors.Is(err, core.ErrBadSetting) {
			t.Errorf("значение %q: %v", v, err)
		}
	}
	if _, err := e.c.SetSetting(ctx, owner, "notify.security", "true"); err != nil {
		t.Errorf("включить можно: %v", err)
	}
	if _, err := e.c.SetSetting(ctx, owner, "notify.payments", "false"); err != nil {
		t.Errorf("обычную тему выключить можно: %v", err)
	}
}

// Пакет докупки с опечаткой: настройка не сохранена, владелец видит пример. Подтверждения «Да» нет: плохое значение
// отклоняется до запроса.
func TestAddonsSettingFormat(t *testing.T) {
	ctx := context.Background()
	e := newCfgEnv(t)
	before := len(e.st.Tasks())
	_, err := e.c.SetSetting(ctx, owner, "addons.traffic", "10ГБ:150")
	var d interface{ Detail() string }
	if !errors.Is(err, core.ErrBadSetting) || !errors.As(err, &d) || !strings.Contains(d.Detail(), "10:15000") {
		t.Fatalf("неверный пакет: %v", err)
	}
	if e.cfg.String("addons.traffic") != "" || len(e.st.Tasks()) != before {
		t.Errorf("плохое значение сохранилось или ушло на «Да»")
	}
	// Верный формат: пакеты денежные, меняются через «Да».
	res, err := e.c.SetSetting(ctx, owner, "addons.traffic", "10:15000,50:50000")
	if err != nil || !res.Pending || e.cfg.String("addons.traffic") != "" {
		t.Fatalf("%+v %v", res, err)
	}
	confirmLast(t, e, true)
	if e.cfg.String("addons.traffic") != "10:15000,50:50000" {
		t.Errorf("после «Да»: %q", e.cfg.String("addons.traffic"))
	}
	// Цена устройства это деньги: тоже после «Да», неверный тип отклоняется сразу.
	if _, err := e.c.SetSetting(ctx, owner, "addons.device_month", "дорого"); !errors.Is(err, core.ErrBadSetting) {
		t.Errorf("тип цены устройства: %v", err)
	}
	if res, err := e.c.SetSetting(ctx, owner, "addons.device_month", "10000"); err != nil || !res.Pending {
		t.Errorf("цена устройства: %+v %v", res, err)
	}
}

func TestAllowIPsNeedsYes(t *testing.T) {
	ctx := context.Background()
	e := newCfgEnv(t)
	if _, err := e.c.SetSetting(ctx, owner, "admin.allow_ips", "не адрес"); !errors.Is(err, core.ErrBadSetting) {
		t.Fatalf("плохой адрес: %v", err)
	}
	res, err := e.c.SetSetting(ctx, owner, "admin.allow_ips", "203.0.113.5, 10.0.0.0/8")
	if err != nil || !res.Pending {
		t.Fatalf("%+v %v", res, err)
	}
	if e.cfg.String("admin.allow_ips") != "" {
		t.Fatal("список адресов сменился до «Да»")
	}
	confirmLast(t, e, false)
	if e.cfg.String("admin.allow_ips") != "" {
		t.Fatal("список сменился после «Нет»")
	}
	if _, err := e.c.SetSetting(ctx, owner, "admin.allow_ips", "203.0.113.5"); err != nil {
		t.Fatal(err)
	}
	confirmLast(t, e, true)
	if e.cfg.String("admin.allow_ips") != "203.0.113.5" {
		t.Fatalf("после «Да»: %q", e.cfg.String("admin.allow_ips"))
	}
	// Сброс списка тоже ждёт «Да»: угнанная сессия не откроет админку всем.
	res, err = e.c.ResetSetting(ctx, owner, "admin.allow_ips")
	if err != nil || !res.Pending || e.cfg.String("admin.allow_ips") != "203.0.113.5" {
		t.Fatalf("сброс: %+v %v", res, err)
	}
	confirmLast(t, e, true)
	if e.cfg.String("admin.allow_ips") != "" {
		t.Errorf("после «Да» на сброс: %q", e.cfg.String("admin.allow_ips"))
	}
	helperWith(t, e.env, 50, core.PermUsers, core.PermGrant)
	if _, err := e.c.SetSetting(ctx, 50, "admin.allow_ips", "1.1.1.1"); !errors.Is(err, core.ErrOwnerOnly) {
		t.Errorf("помощник: %v", err)
	}
}
