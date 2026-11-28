package texts

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// RefreshEvery это как часто кэш перечитывает таблицу. Спека обещает не больше 5 секунд.
const RefreshEvery = 3 * time.Second

var (
	ErrUnknownKey   = errors.New("текста с таким ключом нет")
	ErrUnknownParam = errors.New("в тексте есть неизвестная подстановка")
	ErrEmptyText    = errors.New("текст не может быть пустым")
	ErrBadLang      = errors.New("язык нужен в виде кода, например ru или en")
	langRe          = regexp.MustCompile(`^[a-z]{2,3}(-[a-z0-9]{2,8})?$`)
	placeholderRe   = regexp.MustCompile(`\{([A-Za-z0-9_]+)\}`)
)

// Row это одна изменённая админом строка.
type Row struct {
	Key, Lang, Value string
}

// Store это хранилище правок. Реализует его пакет store, в тестах стоит фейк в памяти.
type Store interface {
	LoadAll() ([]Row, error)
	Save(key, lang, value string, updatedAt int64) error
	Delete(key, lang string) error
}

type cacheKey struct{ key, lang string }

// Texts отдаёт тексты: правка админа или умолчание из кода.
type Texts struct {
	st          Store
	now         func() time.Time
	defaultLang func() string // язык по умолчанию из настроек

	mu   sync.RWMutex
	vals map[cacheKey]string
}

// New читает таблицу. defaultLang подсказывает, какой язык брать, если нужного перевода нет.
func New(st Store, defaultLang func() string, now func() time.Time) (*Texts, error) {
	t := &Texts{st: st, now: now, defaultLang: defaultLang, vals: map[cacheKey]string{}}
	if err := t.Reload(); err != nil {
		return nil, err
	}
	return t, nil
}

// Reload перечитывает таблицу целиком. Строки с неизвестным ключом пропускаются.
func (t *Texts) Reload() error {
	rows, err := t.st.LoadAll()
	if err != nil {
		return fmt.Errorf("чтение текстов: %w", err)
	}
	vals := make(map[cacheKey]string, len(rows))
	for _, r := range rows {
		if _, ok := find(r.Key); ok {
			vals[cacheKey{r.Key, r.Lang}] = r.Value
		}
	}
	t.mu.Lock()
	t.vals = vals
	t.mu.Unlock()
	return nil
}

// Run перечитывает таблицу на каждый сигнал из tick, пока не отменён ctx.
func (t *Texts) Run(ctx context.Context, tick <-chan time.Time) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick:
			if err := t.Reload(); err != nil {
				slog.Warn("тексты: перечитать не вышло", "err", err)
			}
		}
	}
}

// Set сохраняет правку.
func (t *Texts) Set(key, lang, value string) error {
	if err := t.Check(key, lang, value); err != nil {
		return err
	}
	if err := t.st.Save(key, lang, value, t.now().Unix()); err != nil {
		return fmt.Errorf("запись текста %s: %w", key, err)
	}
	t.Apply(key, lang, value)
	return nil
}

// Check проверяет правку, ничего не записывая: ключ, язык, пустой текст, подстановки. Подстановки вне списка
// ключа отклоняются, в ошибке перечислены допустимые. Ядро пишет правку своей транзакцией и зовёт Apply.
func (t *Texts) Check(key, lang, value string) error {
	def, ok := find(key)
	if !ok {
		return fmt.Errorf("%w: %s", ErrUnknownKey, key)
	}
	if !langRe.MatchString(lang) {
		return ErrBadLang
	}
	if strings.TrimSpace(value) == "" {
		return ErrEmptyText
	}
	return checkParams(def, value)
}

// Apply применяет правку в кэше. Зовётся после того, как запись попала в базу.
func (t *Texts) Apply(key, lang, value string) {
	t.mu.Lock()
	t.vals[cacheKey{key, lang}] = value
	t.mu.Unlock()
}

// ApplyReset убирает правку из кэша. Зовётся после удаления строки из базы.
func (t *Texts) ApplyReset(key, lang string) {
	t.mu.Lock()
	delete(t.vals, cacheKey{key, lang})
	t.mu.Unlock()
}

// CheckKey говорит, есть ли такой текст.
func (t *Texts) CheckKey(key string) error {
	if _, ok := find(key); !ok {
		return fmt.Errorf("%w: %s", ErrUnknownKey, key)
	}
	return nil
}

// checkParams ищет в тексте подстановки, которых нет у ключа.
func checkParams(def Def, value string) error {
	allowed := map[string]bool{}
	for _, p := range def.Params {
		allowed[p] = true
	}
	for _, m := range placeholderRe.FindAllStringSubmatch(value, -1) {
		if !allowed[m[1]] {
			return fmt.Errorf("%w {%s}; допустимые для %s: %s", ErrUnknownParam, m[1], def.Key, listParams(def))
		}
	}
	return nil
}

func listParams(def Def) string {
	if len(def.Params) == 0 {
		return "подстановок нет"
	}
	out := make([]string, len(def.Params))
	for i, p := range def.Params {
		out[i] = "{" + p + "}"
	}
	return strings.Join(out, ", ")
}

// Reset убирает правку: дальше действует текст по умолчанию.
func (t *Texts) Reset(key, lang string) error {
	if _, ok := find(key); !ok {
		return fmt.Errorf("%w: %s", ErrUnknownKey, key)
	}
	if err := t.st.Delete(key, lang); err != nil {
		return fmt.Errorf("сброс текста %s: %w", key, err)
	}
	t.mu.Lock()
	delete(t.vals, cacheKey{key, lang})
	t.mu.Unlock()
	return nil
}

// Override отдаёт правку админа для языка. Правки нет: false, и действует умолчание из кода.
func (t *Texts) Override(key, lang string) (string, bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	v, ok := t.vals[cacheKey{key, lang}]
	return v, ok
}

// Langs отдаёт языки, на которых есть тексты: русский и английский из кода и языки правок админа.
func (t *Texts) Langs() []string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	seen := map[string]bool{codeLang: true, enLang: true}
	out := []string{codeLang, enLang}
	var extra []string
	for k := range t.vals {
		if !seen[k.lang] {
			seen[k.lang] = true
			extra = append(extra, k.lang)
		}
	}
	sort.Strings(extra)
	return append(out, extra...)
}

// builtin отдаёт умолчание из кода для языка: русский или английский.
func (d Def) builtin(lang string) (string, bool) {
	switch lang {
	case codeLang:
		return d.Default, true
	case enLang:
		return d.En, true
	}
	return "", false
}

// Raw отдаёт текст без подстановок. Порядок: правка админа и умолчание на языке абонента,
// то же на языке по умолчанию из настроек, русский текст из кода. Для неизвестного ключа
// отдаёт сам ключ, чтобы ошибку было видно.
func (t *Texts) Raw(key, lang string) string {
	def, ok := find(key)
	if !ok {
		return key
	}
	t.mu.RLock()
	defer t.mu.RUnlock()
	for _, l := range []string{lang, t.defaultLang()} {
		if v, ok := t.vals[cacheKey{key, l}]; ok {
			return v
		}
		if v, ok := def.builtin(l); ok {
			return v
		}
	}
	return def.Default
}

// Render отдаёт текст и подставляет значения вида {days}. Подстановки, которых нет в vars, остаются как есть.
// Подставленные значения повторно не разбираются.
func (t *Texts) Render(key, lang string, vars map[string]string) string {
	pairs := make([]string, 0, len(vars)*2)
	for k, v := range vars {
		pairs = append(pairs, "{"+k+"}", v)
	}
	return strings.NewReplacer(pairs...).Replace(t.Raw(key, lang))
}
