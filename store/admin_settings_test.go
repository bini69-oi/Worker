package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/bini69-oi/worker/core"
	"github.com/bini69-oi/worker/core/coretest"
	"github.com/bini69-oi/worker/settings"
	"github.com/bini69-oi/worker/texts"
)

// Правка настроек и текстов из админки переживает перезапуск: ядро пишет их в базу вместе с журналом.
func TestAdminSettingsSurviveRestart(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "w.db")
	key := settings.NewKey()
	open := func() (*core.Core, *DB, *settings.Settings, *texts.Texts) {
		db, err := Open(path)
		if err != nil {
			t.Fatal(err)
		}
		cfg, err := settings.New(NewSettingsStore(db), key, time.Now)
		if err != nil {
			t.Fatal(err)
		}
		tx, err := texts.New(NewTextsStore(db), func() string { return "ru" }, time.Now)
		if err != nil {
			t.Fatal(err)
		}
		c := core.New(core.Deps{Store: NewCoreStore(db), Clock: coretest.NewClock(tNow), Config: cfg, Texts: tx, OwnerID: 1})
		return c, db, cfg, tx
	}
	c, db, cfg, _ := open()
	if _, err := c.SetSetting(ctx, 1, "trial.days", "9"); err != nil {
		t.Fatal(err)
	}
	if err := c.SetText(ctx, 1, "buy.success", "ru", "Готово {days}"); err != nil {
		t.Fatal(err)
	}
	if err := c.SetMaintenance(ctx, 1, true, "Работы"); err != nil {
		t.Fatal(err)
	}
	res, err := c.SetSetting(ctx, 1, "yookassa.secret_key", "live_0123456789abcdefXYZ1")
	if err != nil || !res.Pending {
		t.Fatal(res, err)
	}
	if err := c.AnswerConfirm(ctx, res.ConfirmID, 1, true); err != nil {
		t.Fatal(err)
	}
	_ = cfg
	db.Close()

	_, db, cfg, tx := open()
	defer db.Close()
	if cfg.Int("trial.days") != 9 || !cfg.Bool("maintenance.on") || cfg.Secret("yookassa.secret_key") != "live_0123456789abcdefXYZ1" {
		t.Errorf("настройки после перезапуска: %d %v", cfg.Int("trial.days"), cfg.Bool("maintenance.on"))
	}
	if tx.Raw("buy.success", "ru") != "Готово {days}" || tx.Raw("err.maintenance", "ru") != "Работы" {
		t.Errorf("тексты после перезапуска: %q", tx.Raw("buy.success", "ru"))
	}
	rows, err := NewCoreStore(db).AuditLog(ctx, core.AuditFilter{}, 50)
	if err != nil {
		t.Fatal(err)
	}
	actions := map[string]bool{}
	for _, r := range rows {
		actions[r.Action] = true
	}
	for _, want := range []string{"setting.set", "text.set", "maintenance.on", "secret.set"} {
		if !actions[want] {
			t.Errorf("нет записи журнала %s", want)
		}
	}
}
