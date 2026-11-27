package core_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/bini69-oi/worker/core"
	"github.com/bini69-oi/worker/pay/manual"
	"github.com/bini69-oi/worker/pay/stars"
)

// gwEnv это ядро с ручной кассой и Stars без ключей.
func gwEnv(t *testing.T) (*cfgEnv, *core.Core) {
	t.Helper()
	e := newCfgEnv(t)
	c := core.New(core.Deps{Store: e.st, Clock: e.clock, Config: e.cfg, Panel: e.panel, Notifier: e.notes, OwnerID: owner,
		Texts: e.tx, Gateways: []core.Gateway{manual.New(e.cfg), stars.New(e.cfg)}})
	return e, c
}

func TestEnableGatewayWithoutKeys(t *testing.T) {
	ctx := context.Background()
	e, c := gwEnv(t)
	if e.cfg.String("manual.details") != "" {
		t.Fatal("реквизиты не пусты")
	}
	if _, err := c.SetSetting(ctx, owner, "manual.enabled", "true"); !errors.Is(err, core.ErrNoKeys) {
		t.Fatalf("касса без ключей включилась: %v", err)
	}
	if e.cfg.Bool("manual.enabled") {
		t.Fatal("настройка сохранилась")
	}
	// Выключить и сбросить можно всегда, включить после заполнения ключей можно.
	if _, err := c.SetSetting(ctx, owner, "manual.enabled", "false"); err != nil {
		t.Errorf("выключение: %v", err)
	}
	if err := e.cfg.Set("manual.details", "Карта 0000"); err != nil {
		t.Fatal(err)
	}
	res, err := c.SetSetting(ctx, owner, "manual.enabled", "true")
	if err != nil || !res.Pending || e.cfg.Bool("manual.enabled") {
		t.Fatalf("включение после ключей ждёт «Да»: %+v %v", res, err)
	}
	if err := c.AnswerConfirm(ctx, res.ConfirmID, owner, true); err != nil || !e.cfg.Bool("manual.enabled") {
		t.Errorf("после «Да»: %v", err)
	}
}

func TestGatewayStates(t *testing.T) {
	ctx := context.Background()
	e, c := gwEnv(t)
	if err := e.cfg.Set("manual.details", "Карта 0000"); err != nil {
		t.Fatal(err)
	}
	for k, v := range map[string]string{"manual.enabled": "true", "manual.sort": "2", "stars.sort": "1"} {
		if err := e.cfg.Set(k, v); err != nil {
			t.Fatal(err)
		}
	}
	p1, _, err := c.CreatePayment(ctx, e.user.ID, "manual", 10000)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := c.CreatePayment(ctx, e.user.ID, "manual", 20000); err != nil {
		t.Fatal(err)
	}
	e.clock.Add(time.Hour)
	if err := c.ConfirmManual(ctx, owner, p1.ID); err != nil {
		t.Fatal(err)
	}
	got, err := c.GatewayStates(ctx, owner)
	if err != nil || len(got) != 2 {
		t.Fatalf("%+v %v", got, err)
	}
	if got[0].Name != "stars" || got[1].Name != "manual" {
		t.Errorf("порядок из настроек: %s, %s", got[0].Name, got[1].Name)
	}
	m := got[1]
	if !m.Enabled || !m.Configured || m.Pending != 1 || !m.LastPaidAt.Equal(e.clock.Now()) {
		t.Errorf("manual: %+v", m)
	}
	if got[0].Enabled || got[0].LastPaidAt != (time.Time{}) || got[0].Pending != 0 {
		t.Errorf("stars: %+v", got[0])
	}
	helperWith(t, e.env, 50, core.PermStats)
	if _, err := c.GatewayStates(ctx, 50); !errors.Is(err, core.ErrOwnerOnly) {
		t.Errorf("помощник: %v", err)
	}
}
