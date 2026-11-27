package bot

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode"

	"github.com/bini69-oi/worker/core"
	"github.com/bini69-oi/worker/texts"
	"github.com/bini69-oi/worker/tg"
)

func TestStartMenu(t *testing.T) {
	e := newEnv(t)
	e.set(t, "trial.enabled", "true", "trial.days", "3")
	e.text(t, userTg, "/start")
	golden(t, "menu_new", e.api.take())

	u, _ := e.c.UserByTg(context.Background(), userTg)
	if u.Lang != "ru" {
		t.Fatalf("язык не из Telegram: %q", u.Lang)
	}
	e.press(t, userTg, 101, "trial")
	e.api.take()
	e.press(t, userTg, 101, "m")
	golden(t, "menu_trial_taken", e.api.take())
}

// startAs шлёт /start от абонента с кодом языка Telegram code.
func (e *env) startAs(t *testing.T, code string) core.User {
	t.Helper()
	m := &tg.Message{MessageID: int(e.next()), From: &tg.User{ID: userTg, FirstName: "John", Username: "john", LanguageCode: code},
		Chat: tg.Chat{ID: userTg, Type: "private"}, Text: "/start"}
	if err := e.b.handle(context.Background(), tg.Update{UpdateID: e.upd, Message: m}); err != nil {
		t.Fatal(err)
	}
	u, err := e.c.UserByTg(context.Background(), userTg)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func TestLangFromTelegramCode(t *testing.T) {
	for code, want := range map[string]string{
		"ru": "ru", "uk": "ru", "be": "ru", "kk": "ru", "RU": "ru", "kk-KZ": "ru",
		"de": "en", "en": "en", "en-US": "en", "pt-BR": "en",
	} {
		e := newEnv(t)
		if u := e.startAs(t, code); u.Lang != want {
			t.Errorf("код %q: язык %q, ждали %q", code, u.Lang, want)
		}
	}
}

func TestLangEmptyCode(t *testing.T) {
	e := newEnv(t)
	if got := e.b.lang(""); got != "ru" {
		t.Fatalf("без настройки ждали ru, получили %q", got)
	}
	e.set(t, "lang.default", "en")
	if got := e.b.lang(""); got != "en" {
		t.Fatalf("ждали язык из настройки, получили %q", got)
	}
	if got := e.b.lang("kk"); got != "ru" {
		t.Fatalf("код kk должен давать ru, получили %q", got)
	}
}

func TestEnglishScreens(t *testing.T) {
	e := newEnv(t)
	e.set(t, "trial.enabled", "true", "trial.days", "3")
	u := e.startAs(t, "de")
	golden(t, "menu_new_en", e.api.take())

	tr := e.tariff(t, "Month", core.Period{Days: 30, Price: 25000})
	e.topup(t, u.ID, 30000)
	if _, err := e.c.Buy(context.Background(), u.ID, tr.ID, 30); err != nil {
		t.Fatal(err)
	}
	e.sync(t)
	e.press(t, userTg, 101, "sub")
	golden(t, "sub_active_en", e.api.take())
}

func TestLangButtonAlwaysVisible(t *testing.T) {
	e := newEnv(t)
	e.start(t)
	e.press(t, userTg, 5, "m")
	if !strings.Contains(render(e.api.take()), "→ lang]") {
		t.Fatal("кнопки языка нет: языков два, она видна всегда")
	}
	e.press(t, userTg, 5, "ln:en")
	got := render(e.api.take())
	if !strings.Contains(got, "Main menu") {
		t.Fatalf("меню не на английском: %s", got)
	}
	e.press(t, userTg, 5, "ln:ru")
	if got := render(e.api.take()); !strings.Contains(got, "Главное меню") {
		t.Fatalf("меню не вернулось на русский: %s", got)
	}
}

func TestRefLink(t *testing.T) {
	e := newEnv(t)
	inviter := e.start(t)
	friend := int64(2002)
	e.text(t, friend, "/start ref_"+inviter.RefCode)
	u, err := e.c.UserByTg(context.Background(), friend)
	if err != nil || u.ReferrerID != inviter.ID {
		t.Fatalf("пригласивший не записан: %d (%v)", u.ReferrerID, err)
	}
}

func TestPromoLink(t *testing.T) {
	e := newEnv(t)
	e.start(t)
	if _, err := e.c.SavePromo(context.Background(), owner, core.Promo{Code: "SPRING26", Kind: core.PromoMoney, Value: 10000, Active: true}); err != nil {
		t.Fatal(err)
	}
	e.text(t, userTg, "/start promo_SPRING26")
	got := render(e.api.take())
	if !strings.Contains(got, "+100 ₽ на баланс") || !strings.Contains(got, "Баланс: <b>100 ₽</b>") {
		t.Fatalf("промокод ссылкой: %s", got)
	}
}

func TestStartGarbage(t *testing.T) {
	e := newEnv(t)
	e.start(t)
	for _, arg := range []string{"<script>", strings.Repeat("a", 70), "ref_", "paid_abc", "promo_a b"} {
		e.clock.Add(time.Second) // не упираться в защиту от флуда
		e.text(t, userTg, "/start "+arg)
		if got := render(e.api.take()); !strings.Contains(got, "Главное меню") {
			t.Fatalf("%q: не меню: %s", arg, got)
		}
	}
}

func FuzzStartArg(f *testing.F) {
	for _, s := range []string{"ref_abc", "promo_X-1", "paid_42", "paid_-1", "_", "ref_<x>"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		kind, val := startArg(s)
		if kind == "" {
			return
		}
		if len(s) > maxStartArg || !word(val, 1, 32, true) || (kind != "ref" && kind != "promo" && kind != "paid") {
			t.Fatalf("принято плохое: %q", s)
		}
	})
}

func TestSubScreens(t *testing.T) {
	e := newEnv(t)
	u := e.start(t)
	e.press(t, userTg, 5, "sub")
	golden(t, "sub_none", e.api.take())

	tr := e.tariff(t, "Месяц", core.Period{Days: 30, Price: 25000})
	e.topup(t, u.ID, 30000)
	if _, err := e.c.Buy(context.Background(), u.ID, tr.ID, 30); err != nil {
		t.Fatal(err)
	}
	e.sync(t)
	e.press(t, userTg, 5, "sub")
	golden(t, "sub_active", e.api.take())
}

func TestTrialChannel(t *testing.T) {
	e := newEnv(t)
	e.set(t, "trial.enabled", "true", "trial.days", "3", "trial.channel", "@news")
	e.start(t)
	e.api.member = "left"
	e.press(t, userTg, 5, "trial")
	golden(t, "trial_channel", e.api.take())

	e.api.member = "member"
	e.press(t, userTg, 5, "trial")
	golden(t, "trial_done", e.api.take())
}

func TestBuyFlow(t *testing.T) {
	e := newEnv(t)
	u := e.start(t)
	tr := e.tariff(t, "Месяц", core.Period{Days: 30, Price: 25000})
	e.topup(t, u.ID, 10000)
	e.press(t, userTg, 5, "buy")
	e.press(t, userTg, 5, "bt:"+strconv.FormatInt(tr.ID, 10))
	e.api.take()
	e.press(t, userTg, 5, "bp:"+strconv.FormatInt(tr.ID, 10)+":30")
	golden(t, "buy_need", e.api.take())

	e.topup(t, u.ID, 20000)
	e.press(t, userTg, 5, "bp:"+strconv.FormatInt(tr.ID, 10)+":30")
	golden(t, "buy_confirm", e.api.take())

	// Двойной клик по «Оплатить»: списано один раз.
	ok := "bok:" + strconv.FormatInt(tr.ID, 10) + ":30"
	e.press(t, userTg, 5, ok)
	e.press(t, userTg, 5, ok)
	calls := e.api.take()
	if !strings.Contains(render(calls), "Уже обрабатываем") {
		t.Fatalf("второй клик не отбит: %s", render(calls))
	}
	if got, _ := e.c.UserByTg(context.Background(), userTg); got.Balance != 5000 {
		t.Fatalf("баланс после двойного клика %d, ждали 5000", got.Balance)
	}
}

func TestBuyOtherTariff(t *testing.T) {
	e := newEnv(t)
	u := e.start(t)
	a := e.tariff(t, "Месяц", core.Period{Days: 30, Price: 100})
	b := e.tariff(t, "Год", core.Period{Days: 365, Price: 1000})
	e.topup(t, u.ID, 10000)
	if _, err := e.c.Buy(context.Background(), u.ID, a.ID, 30); err != nil {
		t.Fatal(err)
	}
	id := strconv.FormatInt(b.ID, 10)
	e.press(t, userTg, 5, "bp:"+id+":365")
	golden(t, "plan_queue_confirm", e.api.take())
	e.press(t, userTg, 5, "bok:"+id+":365")
	if got := render(e.api.take()); !strings.Contains(got, "Тариф Год начнётся") {
		t.Fatalf("после оплаты смены: %s", got)
	}
	e.press(t, userTg, 5, "sub")
	golden(t, "sub_plan_queued", e.api.take())
	e.press(t, userTg, 5, "pcanok")
	if got, _ := e.c.UserByTg(context.Background(), userTg); got.Balance != 10000-100 {
		t.Fatalf("после отмены баланс %d", got.Balance)
	}
}

func TestDevices(t *testing.T) {
	e := newEnv(t)
	u := e.start(t)
	tr := e.tariff(t, "Месяц", core.Period{Days: 30, Price: 100})
	e.topup(t, u.ID, 1000)
	if _, err := e.c.Buy(context.Background(), u.ID, tr.ID, 30); err != nil {
		t.Fatal(err)
	}
	e.sync(t)
	sub, _ := e.c.Sub(context.Background(), u.ID)
	e.panel.AddFullDevice(sub.PanelUUID, core.PanelDevice{HWID: "h1", Platform: "iOS", Model: "iPhone"})
	e.panel.AddFullDevice(sub.PanelUUID, core.PanelDevice{HWID: "h2", Platform: "Android", Model: "Pixel"})
	e.press(t, userTg, 5, "dev")
	golden(t, "devices", e.api.take())

	// Список изменился с показа: бот не угадывает, а показывает свежий.
	e.press(t, userTg, 5, "devdel:1:3")
	if got := render(e.api.take()); !strings.Contains(got, "Список устройств изменился") {
		t.Fatalf("устаревшая кнопка отвязки: %s", got)
	}
	e.press(t, userTg, 5, "devdel:1:2")
	if got := render(e.api.take()); strings.Contains(got, "iPhone") || !strings.Contains(got, "Pixel") {
		t.Fatalf("отвязано не то устройство: %s", got)
	}
}

func TestBalanceRefPromo(t *testing.T) {
	e := newEnv(t)
	u := e.start(t)
	e.topup(t, u.ID, 50000)
	e.press(t, userTg, 5, "bal")
	golden(t, "balance", e.api.take())
	e.press(t, userTg, 5, "ref")
	got := render(e.api.take())
	if !strings.Contains(got, "https://t.me/workerbot?start=ref_"+u.RefCode) {
		t.Fatalf("реферальная ссылка: %s", got)
	}
	if _, err := e.c.SavePromo(context.Background(), owner, core.Promo{Code: "BONUS7", Kind: core.PromoMoney, Value: 700, Active: true}); err != nil {
		t.Fatal(err)
	}
	e.press(t, userTg, 5, "promo")
	e.text(t, userTg, "bonus7")
	if got := render(e.api.take()); !strings.Contains(got, "+7 ₽ на баланс") {
		t.Fatalf("промокод вводом: %s", got)
	}
}

func TestAutoRenewToggle(t *testing.T) {
	e := newEnv(t)
	u := e.start(t)
	tr := e.tariff(t, "Месяц", core.Period{Days: 30, Price: 100})
	e.topup(t, u.ID, 1000)
	if _, err := e.c.Buy(context.Background(), u.ID, tr.ID, 30); err != nil {
		t.Fatal(err)
	}
	e.press(t, userTg, 5, "ar:1")
	if s, _ := e.c.Sub(context.Background(), u.ID); !s.AutoRenew {
		t.Fatal("автопродление не включилось")
	}
	e.press(t, userTg, 5, "ar:0")
	if s, _ := e.c.Sub(context.Background(), u.ID); s.AutoRenew {
		t.Fatal("автопродление не выключилось")
	}
}

func TestSetupCommands(t *testing.T) {
	e := newEnv(t)
	if err := e.b.Setup(context.Background()); err != nil {
		t.Fatal(err)
	}
	e.set(t, "app.miniapp", "true")
	if err := e.b.Setup(context.Background()); err != nil {
		t.Fatal(err)
	}
	golden(t, "setup", e.api.take())
}

// TestNoUserStringsInCode проверяет, что в коде бота нет строк для абонента: всё через тексты по ключу.
// Строка с кириллицей вне вызова t и вне panic значит текст, который админ не сможет поправить.
func TestNoUserStringsInCode(t *testing.T) {
	files, _ := filepath.Glob("*.go")
	keys := map[string]bool{}
	for _, d := range texts.Defs() {
		keys[d.Key] = true
	}
	fset := token.NewFileSet()
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		file, err := parser.ParseFile(fset, f, src, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if ok {
				if id, isID := call.Fun.(*ast.Ident); isID && id.Name == "panic" {
					return false
				}
				// Журнал и внутренние ошибки абонент не видит.
				if sel, isSel := call.Fun.(*ast.SelectorExpr); isSel {
					if x, isX := sel.X.(*ast.SelectorExpr); isX && x.Sel.Name == "log" {
						return false
					}
					if x, isX := sel.X.(*ast.Ident); isX && x.Name == "errors" && sel.Sel.Name == "New" {
						return false
					}
				}
				if sel, isSel := call.Fun.(*ast.SelectorExpr); isSel && (sel.Sel.Name == "t" || sel.Sel.Name == "btn") && len(call.Args) > 1 {
					if lit, isLit := call.Args[1].(*ast.BasicLit); isLit {
						key, _ := strconv.Unquote(lit.Value)
						if !keys[key] {
							t.Errorf("%s: текста с ключом %q нет в texts", fset.Position(lit.Pos()), key)
						}
					}
				}
			}
			lit, ok := n.(*ast.BasicLit)
			if ok && lit.Kind == token.STRING && strings.IndexFunc(lit.Value, func(r rune) bool { return unicode.Is(unicode.Cyrillic, r) }) >= 0 {
				t.Errorf("%s: строка для абонента в коде: %s", fset.Position(lit.Pos()), lit.Value)
			}
			return true
		})
	}
}

func TestEveryCoreErrorHasText(t *testing.T) {
	keys := map[string]bool{}
	for _, d := range texts.Defs() {
		keys[d.Key] = true
	}
	for _, e := range core.Errors() {
		if !keys[e.Key] {
			t.Errorf("у ошибки %s нет текста %s", e.Code, e.Key)
		}
	}
}

func TestValuesEscaped(t *testing.T) {
	e := newEnv(t)
	e.start(t)
	tr := e.tariff(t, "<b>Хак</b>", core.Period{Days: 30, Price: 100})
	e.press(t, userTg, 5, "bt:"+strconv.FormatInt(tr.ID, 10))
	got := render(e.api.take())
	if !strings.Contains(got, "&lt;b&gt;Хак&lt;/b&gt;") {
		t.Fatalf("название тарифа не экранировано: %s", got)
	}
}
