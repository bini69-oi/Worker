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

// Set сохраняет правку. Подстановки вне списка ключа отклоняются, в ошибке перечислены допустимые.
func (t *Texts) Set(key, lang, value string) error {
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
	if err := checkParams(def, value); err != nil {
		return err
	}
	if err := t.st.Save(key, lang, value, t.now().Unix()); err != nil {
		return fmt.Errorf("запись текста %s: %w", key, err)
	}
	t.mu.Lock()
	t.vals[cacheKey{key, lang}] = value
	t.mu.Unlock()
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

// Langs отдаёт языки, на которых есть тексты: язык умолчаний из кода и языки правок админа.
func (t *Texts) Langs() []string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	seen := map[string]bool{codeLang: true}
	out := []string{codeLang}
	for k := range t.vals {
		if !seen[k.lang] {
			seen[k.lang] = true
			out = append(out, k.lang)
		}
	}
	sort.Strings(out[1:])
	return out
}

// Raw отдаёт текст без подстановок. Порядок: язык абонента, язык по умолчанию из настроек,
// русский текст из кода. Для неизвестного ключа отдаёт сам ключ, чтобы ошибку было видно.
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
		if l == codeLang {
			return def.Default
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
