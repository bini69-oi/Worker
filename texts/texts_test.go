package texts

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"
)

type fakeStore struct {
	mu   sync.Mutex
	rows map[cacheKey]string
}

func newFake() *fakeStore { return &fakeStore{rows: map[cacheKey]string{}} }

func (f *fakeStore) LoadAll() ([]Row, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []Row
	for k, v := range f.rows {
		out = append(out, Row{k.key, k.lang, v})
	}
	return out, nil
}

func (f *fakeStore) Save(key, lang, value string, _ int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rows[cacheKey{key, lang}] = value
	return nil
}

func (f *fakeStore) Delete(key, lang string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.rows, cacheKey{key, lang})
	return nil
}

var t0 = time.Unix(1_700_000_000, 0)

func newTexts(t *testing.T, defLang string) (*Texts, *fakeStore) {
	t.Helper()
	f := newFake()
	tx, err := New(f, func() string { return defLang }, func() time.Time { return t0 })
	if err != nil {
		t.Fatal(err)
	}
	return tx, f
}

func TestDefaultsValid(t *testing.T) {
	seen := map[string]bool{}
	for _, d := range defs {
		if seen[d.Key] || d.Default == "" {
			t.Errorf("ключ %s повторён или без текста", d.Key)
		}
		seen[d.Key] = true
		if err := checkParams(d, d.Default); err != nil {
			t.Errorf("умолчание %s: %v", d.Key, err)
		}
	}
}

func TestDefaultText(t *testing.T) {
	tx, _ := newTexts(t, "ru")
	if got := tx.Raw("buy.success", "ru"); got != defs[0].Default {
		t.Fatalf("ждали текст по умолчанию, получили %q", got)
	}
}

func TestChangedTextVisible(t *testing.T) {
	tx, f := newTexts(t, "ru")
	if err := tx.Set("buy.success", "ru", "Готово, дней: {days}"); err != nil {
		t.Fatal(err)
	}
	if got := tx.Raw("buy.success", "ru"); got != "Готово, дней: {days}" {
		t.Fatalf("правка не видна: %q", got)
	}

	// Правка другим процессом подхватывается по сигналу перечитывания.
	f.Save("buy.success", "ru", "Новый текст", 0)
	tick := make(chan time.Time)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { tx.Run(ctx, tick); close(done) }()
	tick <- t0
	tick <- t0 // второй сигнал принят, значит первое перечитывание закончено
	if got := tx.Raw("buy.success", "ru"); got != "Новый текст" {
		t.Fatalf("после перечитывания ждали новый текст, получили %q", got)
	}
	cancel()
	<-done
	if RefreshEvery >= 5*time.Second {
		t.Fatal("период перечитывания должен быть меньше 5 секунд")
	}
}

func TestPlaceholderSubstitution(t *testing.T) {
	tx, _ := newTexts(t, "ru")
	if got := tx.Render("sub.expiring", "ru", map[string]string{"days": "5"}); got != "Осталось 5 дн." {
		t.Fatalf("получили %q", got)
	}
}

func TestSubstitutedValueNotReparsed(t *testing.T) {
	tx, _ := newTexts(t, "ru")
	got := tx.Render("sub.expiring", "ru", map[string]string{"days": "{balance}", "balance": "X"})
	if got != "Осталось {balance} дн." {
		t.Fatalf("подставленное значение разобрано повторно: %q", got)
	}
}

func TestUnknownPlaceholderRejected(t *testing.T) {
	tx, f := newTexts(t, "ru")
	err := tx.Set("sub.expiring", "ru", "Осталось {foo} дн.")
	if !errors.Is(err, ErrUnknownParam) {
		t.Fatalf("ждали ErrUnknownParam, получили %v", err)
	}
	if !strings.Contains(err.Error(), "{days}") {
		t.Fatalf("в подсказке нет допустимых подстановок: %v", err)
	}
	if len(f.rows) != 0 {
		t.Fatal("отклонённый текст попал в базу")
	}
}

func TestEnglishDefault(t *testing.T) {
	tx, _ := newTexts(t, "ru")
	if got := tx.Raw("sub.expiring", "en"); got != "{days} days left" {
		t.Fatalf("без правок ждали английское умолчание, получили %q", got)
	}
	if got := tx.Raw("sub.expiring", "ru"); got != "Осталось {days} дн." {
		t.Fatalf("русское умолчание сломано: %q", got)
	}
}

func TestFallbackLanguage(t *testing.T) {
	tx, _ := newTexts(t, "ru")
	// Для языка без перевода берётся язык по умолчанию из настроек.
	if got := tx.Raw("sub.expiring", "de"); got != "Осталось {days} дн." {
		t.Fatalf("без перевода ждали русский, получили %q", got)
	}
	// Русская правка не перебивает английское умолчание.
	if err := tx.Set("sub.expiring", "ru", "Дней: {days}"); err != nil {
		t.Fatal(err)
	}
	if got := tx.Raw("sub.expiring", "en"); got != "{days} days left" {
		t.Fatalf("ждали английское умолчание, получили %q", got)
	}
	if got := tx.Raw("sub.expiring", "de"); got != "Дней: {days}" {
		t.Fatalf("ждали правку русского для немецкого, получили %q", got)
	}
	if err := tx.Set("sub.expiring", "en", "Days left: {days}"); err != nil {
		t.Fatal(err)
	}
	if got := tx.Raw("sub.expiring", "en"); got != "Days left: {days}" {
		t.Fatalf("ждали английскую правку, получили %q", got)
	}
}

func TestFallbackToNonRuDefaultLang(t *testing.T) {
	tx, _ := newTexts(t, "en")
	if got := tx.Raw("sub.expiring", "de"); got != "{days} days left" {
		t.Fatalf("ждали английское умолчание, получили %q", got)
	}
	if err := tx.Set("sub.expiring", "en", "Days left: {days}"); err != nil {
		t.Fatal(err)
	}
	if got := tx.Raw("sub.expiring", "de"); got != "Days left: {days}" {
		t.Fatalf("ждали язык по умолчанию из настроек, получили %q", got)
	}
}

func TestLangs(t *testing.T) {
	tx, _ := newTexts(t, "ru")
	if got := strings.Join(tx.Langs(), ","); got != "ru,en" {
		t.Fatalf("получили %s", got)
	}
	if err := tx.Set("buy.success", "de", "Fertig {days}"); err != nil {
		t.Fatal(err)
	}
	if err := tx.Set("buy.success", "en", "Done {days}"); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(tx.Langs(), ","); got != "ru,en,de" {
		t.Fatalf("получили %s", got)
	}
}

func paramSet(s string) map[string]bool {
	out := map[string]bool{}
	for _, m := range placeholderRe.FindAllStringSubmatch(s, -1) {
		out[m[1]] = true
	}
	return out
}

var tagRe = regexp.MustCompile(`</?([a-z]+)>`)

func TestEnglishComplete(t *testing.T) {
	for _, d := range defs {
		if strings.TrimSpace(d.En) == "" {
			t.Errorf("%s: нет английского текста", d.Key)
			continue
		}
		if err := checkParams(d, d.En); err != nil {
			t.Errorf("%s: %v", d.Key, err)
		}
		// Набор в En равен набору в Default; Params допускает лишние, бот передаёт их про запас.
		got := paramSet(d.En)
		ru := paramSet(d.Default)
		if len(got) != len(ru) {
			t.Errorf("%s: подстановки En %v не равны Default %v", d.Key, got, ru)
		}
		for p := range ru {
			if !got[p] {
				t.Errorf("%s: в En нет {%s}", d.Key, p)
			}
		}
		open := map[string]int{}
		for _, m := range tagRe.FindAllStringSubmatch(d.En, -1) {
			if strings.HasPrefix(m[0], "</") {
				open[m[1]]--
			} else {
				open[m[1]]++
			}
		}
		for tag, n := range open {
			if n != 0 {
				t.Errorf("%s: тег <%s> не закрыт в En", d.Key, tag)
			}
		}
		if strings.HasPrefix(d.Key, "btn.") && utf8.RuneCountInString(d.En) > 32 {
			t.Errorf("%s: кнопка длиннее 32 символов: %q", d.Key, d.En)
		}
	}
}

func TestResetToDefault(t *testing.T) {
	tx, f := newTexts(t, "ru")
	if err := tx.Set("buy.success", "ru", "Моё {days}"); err != nil {
		t.Fatal(err)
	}
	if err := tx.Reset("buy.success", "ru"); err != nil {
		t.Fatal(err)
	}
	if len(f.rows) != 0 || tx.Raw("buy.success", "ru") != defs[0].Default {
		t.Fatal("после сброса должен действовать текст по умолчанию")
	}
}

func TestUnknownKeyAndBadInput(t *testing.T) {
	tx, _ := newTexts(t, "ru")
	if err := tx.Set("нет.такого", "ru", "x"); !errors.Is(err, ErrUnknownKey) {
		t.Errorf("ждали ErrUnknownKey, получили %v", err)
	}
	if err := tx.Set("buy.success", "Русский", "x"); !errors.Is(err, ErrBadLang) {
		t.Errorf("ждали ErrBadLang, получили %v", err)
	}
	if err := tx.Set("buy.success", "ru", "  "); !errors.Is(err, ErrEmptyText) {
		t.Errorf("ждали ErrEmptyText, получили %v", err)
	}
	if got := tx.Raw("нет.такого", "ru"); got != "нет.такого" {
		t.Errorf("для неизвестного ключа ждали сам ключ, получили %q", got)
	}
}

func TestByPrefix(t *testing.T) {
	tx, f := newTexts(t, "ru")
	got := tx.ByPrefix("en", "err.", "bot.web_")
	if got["err.not_found"] != "Not found" || got["bot.web_login_ok"] == "" {
		t.Fatalf("английские тексты: %v", got)
	}
	if _, ok := got["notify.remind"]; ok {
		t.Fatal("в ответ попал ключ вне префиксов")
	}
	f.rows[cacheKey{"err.not_found", "en"}] = "Nothing here"
	if err := tx.Reload(); err != nil {
		t.Fatal(err)
	}
	if got := tx.ByPrefix("en", "err."); got["err.not_found"] != "Nothing here" {
		t.Fatalf("правка админа не видна: %v", got["err.not_found"])
	}
	if got := tx.ByPrefix("de", "err."); got["err.not_found"] == "" {
		t.Fatal("для неизвестного языка нет текста по умолчанию")
	}
}
