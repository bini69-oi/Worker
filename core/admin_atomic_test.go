package core_test

import (
	"context"
	"errors"
	"testing"

	"github.com/bini69-oi/worker/core"
	"github.com/bini69-oi/worker/core/coretest"
)

// brokenTx это хранилище, у которого транзакции записи не проходят: так выглядит сбой базы при правке.
type brokenTx struct{ *coretest.Store }

func (brokenTx) Tx(context.Context, func(core.Tx) error) error {
	return errors.New("сбой базы")
}

// Правка и журнал идут одной транзакцией: при сбое не меняется ни память, ни настройки, ни тексты.
func TestOwnerChangesAreAtomic(t *testing.T) {
	ctx := context.Background()
	e := newCfgEnv(t)
	c := core.New(core.Deps{Store: brokenTx{e.st}, Clock: e.clock, Config: e.cfg, Panel: e.panel, Notifier: e.notes, OwnerID: owner, Texts: e.tx})
	before := e.tx.Raw("buy.success", "ru")
	if err := c.SetText(ctx, owner, "buy.success", "ru", "ПОДМЕНА {days}"); err == nil {
		t.Error("сбой базы не дошёл до вызывающего")
	}
	if e.tx.Raw("buy.success", "ru") != before {
		t.Error("текст изменился без записи в базе и журнал")
	}
	if _, err := c.SetSetting(ctx, owner, "trial.days", "9"); err == nil || e.cfg.Int("trial.days") != 3 {
		t.Errorf("настройка изменилась без записи: %v", err)
	}
	if err := c.SetMaintenance(ctx, owner, true, "Работы"); err == nil || c.Maintenance() || e.tx.Raw("bot.maintenance", "ru") == "Работы" {
		t.Errorf("техработы включились без записи: %v", err)
	}
}

// Правка пишется в базу той же транзакцией, что и журнал, а память обновляется после коммита.
func TestOwnerChangesWrittenWithAudit(t *testing.T) {
	ctx := context.Background()
	e := newCfgEnv(t)
	if err := e.c.SetText(ctx, owner, "buy.success", "ru", "Готово {days}"); err != nil {
		t.Fatal(err)
	}
	if v, ok := e.st.StoredText("buy.success", "ru"); !ok || v != "Готово {days}" {
		t.Errorf("текст в базе: %q %v", v, ok)
	}
	if _, err := e.c.SetSetting(ctx, owner, "trial.days", "9"); err != nil {
		t.Fatal(err)
	}
	if v, ok := e.st.StoredSetting("trial.days"); !ok || v != "9" {
		t.Errorf("настройка в базе: %q %v", v, ok)
	}
	if _, err := e.c.ResetSetting(ctx, owner, "trial.days"); err != nil {
		t.Fatal(err)
	}
	if _, ok := e.st.StoredSetting("trial.days"); ok {
		t.Error("после сброса строка осталась")
	}
	if err := e.c.ResetText(ctx, owner, "buy.success", "ru"); err != nil {
		t.Fatal(err)
	}
	if _, ok := e.st.StoredText("buy.success", "ru"); ok {
		t.Error("после сброса правка текста осталась")
	}
	if err := e.c.SetMaintenance(ctx, owner, true, "Работы <b>до 15:00</b>"); err != nil {
		t.Fatal(err)
	}
	if v, ok := e.st.StoredSetting("maintenance.on"); !ok || v != "true" {
		t.Errorf("техработы в базе: %q %v", v, ok)
	}
	if v, ok := e.st.StoredText("err.maintenance", "ru"); !ok || v != "Работы <b>до 15:00</b>" {
		t.Errorf("текст техработ в базе: %q %v", v, ok)
	}
}

// Подтверждённый секрет пишется в базу шифротекстом вместе с журналом, значения в журнале нет.
func TestConfirmedSecretStoredWithAudit(t *testing.T) {
	ctx := context.Background()
	e := newCfgEnv(t)
	const secret = "live_0123456789abcdefXYZ1"
	res, err := e.c.SetSetting(ctx, owner, "yookassa.secret_key", secret)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.c.AnswerConfirm(ctx, res.ConfirmID, owner, true); err != nil {
		t.Fatal(err)
	}
	stored, ok := e.st.StoredSetting("yookassa.secret_key")
	if !ok || stored == secret || stored == "" {
		t.Fatalf("в базе должен лежать шифротекст: %q %v", stored, ok)
	}
	if e.cfg.Secret("yookassa.secret_key") != secret {
		t.Error("секрет не применился")
	}
	found := false
	for _, a := range e.st.Audit() {
		found = found || (a.Action == "secret.set" && a.Target == "yookassa.secret_key")
	}
	if !found {
		t.Error("смена секрета не записана в журнал")
	}
}
