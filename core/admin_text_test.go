package core_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/bini69-oi/worker/core"
	"github.com/bini69-oi/worker/core/coretest"
	"github.com/bini69-oi/worker/texts"
)

// cfgEnv это ядро с редактором текстов.
type cfgEnv struct {
	*env
	c  *core.Core
	tx *texts.Texts
}

func newCfgEnv(t *testing.T) *cfgEnv {
	t.Helper()
	e := newEnv(t)
	if err := e.cfg.Set("summary.tz", "UTC"); err != nil {
		t.Fatal(err)
	}
	tx := coretest.NewTexts(t)
	c := core.New(core.Deps{Store: e.st, Clock: e.clock, Config: e.cfg, Panel: e.panel, Notifier: e.notes, OwnerID: owner, Texts: tx})
	return &cfgEnv{env: e, c: c, tx: tx}
}

func TestSetText(t *testing.T) {
	ctx := context.Background()
	e := newCfgEnv(t)
	if err := e.c.SetText(ctx, owner, "buy.success", "ru", "Готово, ещё {days} дн."); err != nil {
		t.Fatal(err)
	}
	if got := e.tx.Render("buy.success", "ru", map[string]string{"days": "5"}); got != "Готово, ещё 5 дн." {
		t.Errorf("абонент видит %q", got)
	}
	log := e.st.Audit()
	if a := log[len(log)-1]; a.Action != "text.set" || a.Target != "buy.success" || a.Details["lang"] != "ru" {
		t.Errorf("журнал: %+v", a)
	}
}

func TestSetTextTypoInPlaceholder(t *testing.T) {
	ctx := context.Background()
	e := newCfgEnv(t)
	before := e.tx.Raw("buy.success", "ru")
	err := e.c.SetText(ctx, owner, "buy.success", "ru", "Ещё {dayz} дн.")
	var d interface{ Detail() string }
	if !errors.Is(err, core.ErrBadText) || !errors.As(err, &d) || !strings.Contains(d.Detail(), "{days}") {
		t.Fatalf("ошибка должна называть допустимые подстановки: %v", err)
	}
	if e.tx.Raw("buy.success", "ru") != before {
		t.Errorf("текст с опечаткой сохранился")
	}
}

func TestSetTextLimits(t *testing.T) {
	ctx := context.Background()
	e := newCfgEnv(t)
	if err := e.c.SetText(ctx, owner, "btn.ban", "ru", strings.Repeat("я", 65)); !errors.Is(err, core.ErrTooLong) {
		t.Errorf("подпись кнопки 65: %v", err)
	}
	if err := e.c.SetText(ctx, owner, "btn.ban", "ru", strings.Repeat("я", 64)); err != nil {
		t.Errorf("подпись кнопки 64: %v", err)
	}
	if err := e.c.SetText(ctx, owner, "buy.success", "ru", strings.Repeat("я", 4097)); !errors.Is(err, core.ErrTooLong) {
		t.Errorf("сообщение 4097: %v", err)
	}
	if err := e.c.SetText(ctx, owner, "buy.success", "ru", "  "); !errors.Is(err, core.ErrBadText) {
		t.Errorf("пустой текст: %v", err)
	}
	if err := e.c.SetText(ctx, owner, "нет.такого", "ru", "x"); !errors.Is(err, core.ErrNotFound) {
		t.Errorf("неизвестный ключ: %v", err)
	}
	if err := e.c.SetText(ctx, owner, "buy.success", "РУ", "x"); !errors.Is(err, core.ErrInvalid) {
		t.Errorf("неверный язык: %v", err)
	}
}

func TestResetText(t *testing.T) {
	ctx := context.Background()
	e := newCfgEnv(t)
	def := e.tx.Raw("buy.success", "ru")
	if err := e.c.SetText(ctx, owner, "buy.success", "ru", "Другой {days}"); err != nil {
		t.Fatal(err)
	}
	if err := e.c.ResetText(ctx, owner, "buy.success", "ru"); err != nil {
		t.Fatal(err)
	}
	if e.tx.Raw("buy.success", "ru") != def {
		t.Errorf("после сброса текст не равен умолчанию")
	}
	log := e.st.Audit()
	if log[len(log)-1].Action != "text.reset" {
		t.Errorf("журнал: %+v", log[len(log)-1])
	}
}

func TestTextHelperDenied(t *testing.T) {
	ctx := context.Background()
	e := newCfgEnv(t)
	helperWith(t, e.env, 50, core.PermUsers, core.PermBroadcast, core.PermTariffs)
	for name, fn := range map[string]func() error{
		"set":   func() error { return e.c.SetText(ctx, 50, "buy.success", "ru", "x {days}") },
		"reset": func() error { return e.c.ResetText(ctx, 50, "buy.success", "ru") },
		"list":  func() error { _, err := e.c.TextList(ctx, 50); return err },
	} {
		if err := fn(); !errors.Is(err, core.ErrOwnerOnly) {
			t.Errorf("%s: помощнику ждали отказ владельцу, получили %v", name, err)
		}
	}
	if e.tx.Raw("buy.success", "ru") == "x {days}" {
		t.Errorf("текст изменён помощником")
	}
	n := 0
	for _, a := range e.st.Audit() {
		if strings.HasPrefix(a.Action, "denied:text.") {
			n++
		}
	}
	if n != 3 {
		t.Errorf("записей об отказе %d", n)
	}
}

func TestTextList(t *testing.T) {
	ctx := context.Background()
	e := newCfgEnv(t)
	if err := e.c.SetText(ctx, owner, "buy.success", "en", "Paid, {days} left"); err != nil {
		t.Fatal(err)
	}
	list, err := e.c.TextList(ctx, owner)
	if err != nil || len(list) == 0 {
		t.Fatal(err)
	}
	for _, it := range list {
		if it.Key != "buy.success" {
			continue
		}
		if !it.Changed["en"] || it.Changed["ru"] || it.Value["en"] != "Paid, {days} left" || it.Value["ru"] != it.Default["ru"] || len(it.Params) != 1 {
			t.Errorf("%+v", it)
		}
		return
	}
	t.Error("нет ключа buy.success")
}
