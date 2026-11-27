package main

import (
	"strings"
	"testing"

	"github.com/bini69-oi/worker/settings"
)

// Какие настройки требуют «Да» владельца: всё, что касса настраивает сама, кроме названия и порядка,
// и денежные настройки бота. Проверка идёт по всем кассам, которые подключены к процессу.
func TestMoneySettingsNeedYes(t *testing.T) {
	reg := settings.Registry()
	gateways := map[string]bool{}
	for _, d := range reg {
		if name, ok := strings.CutSuffix(d.Key, ".enabled"); ok && d.Group == "pay."+name {
			gateways[name] = true
		}
	}
	if len(gateways) < 20 {
		t.Fatalf("касс в реестре %d: пакеты касс не подключены к тесту", len(gateways))
	}
	for _, d := range reg {
		name, _, _ := strings.Cut(d.Key, ".")
		if !gateways[name] {
			continue
		}
		want := !strings.HasSuffix(d.Key, ".title") && !strings.HasSuffix(d.Key, ".sort")
		if d.Secret {
			want = true
		}
		if got := settings.NeedsConfirm(d.Key); got != want {
			t.Errorf("%s: «Да» требуется %v, ждали %v", d.Key, got, want)
		}
	}
	for _, key := range []string{"manual.details", "stars.rate", "ref.first_percent", "ref.first_fixed", "ref.each_percent",
		"ref.welcome_bonus", "addons.traffic", "addons.device_month", "yookassa.return_url", "wata.return_url",
		"platega.return_url", "yoomoney.return_url", "yookassa.secret_key", "panel.webhook_secret", "admin.allow_ips"} {
		if !settings.NeedsConfirm(key) {
			t.Errorf("%s меняется без «Да»", key)
		}
	}
	for _, key := range []string{"trial.days", "maintenance.on", "connect.apps", "manual.title", "stars.sort", "summary.hour", "notify.payments"} {
		if settings.NeedsConfirm(key) {
			t.Errorf("%s требует «Да» без нужды", key)
		}
	}
}
