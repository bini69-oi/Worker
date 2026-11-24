package texts

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
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

func TestFallbackLanguage(t *testing.T) {
	tx, _ := newTexts(t, "ru")
	if got := tx.Raw("sub.expiring", "en"); got != "Осталось {days} дн." {
		t.Fatalf("без перевода ждали русский, получили %q", got)
	}
	// Русская правка тоже запасная для английского.
	if err := tx.Set("sub.expiring", "ru", "Дней: {days}"); err != nil {
		t.Fatal(err)
	}
	if got := tx.Raw("sub.expiring", "en"); got != "Дней: {days}" {
		t.Fatalf("ждали правку русского, получили %q", got)
	}
	if err := tx.Set("sub.expiring", "en", "Days left: {days}"); err != nil {
		t.Fatal(err)
	}
	if got := tx.Raw("sub.expiring", "en"); got != "Days left: {days}" {
		t.Fatalf("ждали английский, получили %q", got)
	}
}

func TestFallbackToNonRuDefaultLang(t *testing.T) {
	tx, _ := newTexts(t, "en")
	if err := tx.Set("sub.expiring", "en", "Days left: {days}"); err != nil {
		t.Fatal(err)
	}
	if got := tx.Raw("sub.expiring", "de"); got != "Days left: {days}" {
		t.Fatalf("ждали язык по умолчанию из настроек, получили %q", got)
	}
	if got := tx.Raw("buy.success", "de"); got != defs[0].Default {
		t.Fatalf("нет и там: ждали текст из кода, получили %q", got)
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
