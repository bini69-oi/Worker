package settings

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Store это хранилище строк настроек. Реализует его пакет store, в тестах стоит фейк в памяти.
type Store interface {
	LoadAll() (map[string]string, error)
	Save(key, value string, updatedAt int64) error
	Delete(key string) error
}

// Settings держит в памяти значения, отличные от умолчаний.
type Settings struct {
	st  Store
	key []byte // мастер-ключ шифрования секретов, 32 байта
	now func() time.Time

	mu   sync.RWMutex
	vals map[string]string
}

// New читает таблицу и возвращает готовые настройки. Время подставляется снаружи.
// Ключ шифрования нужен ровно 32 байта (AES-256). Если секрет не расшифровывается, New отказывает.
func New(st Store, key []byte, now func() time.Time) (*Settings, error) {
	if len(key) != 32 {
		return nil, errors.New("ключ шифрования должен быть 32 байта")
	}
	s := &Settings{st: st, key: key, now: now, vals: map[string]string{}}
	if err := s.Reload(); err != nil {
		return nil, err
	}
	return s, nil
}

// Reload перечитывает таблицу целиком. Строки с неизвестным ключом или плохим значением
// пропускаются, для них действует умолчание.
func (s *Settings) Reload() error {
	rows, err := s.st.LoadAll()
	if err != nil {
		return fmt.Errorf("чтение настроек: %w", err)
	}
	vals := make(map[string]string, len(rows))
	var bad []string
	for key, raw := range rows {
		def, ok := find(key)
		if !ok {
			continue
		}
		v, err := s.decode(def, raw)
		if err != nil {
			bad = append(bad, key)
			continue
		}
		vals[key] = v
	}
	s.mu.Lock()
	// Значение, которое не прочиталось, остаётся прежним: сбой перечитывания не выключает кассы.
	for _, key := range bad {
		if old, ok := s.vals[key]; ok {
			vals[key] = old
		}
	}
	s.vals = vals
	s.mu.Unlock()
	if len(bad) > 0 {
		sort.Strings(bad)
		return fmt.Errorf("настройки не прочитаны (ключ шифрования или значение): %s", strings.Join(bad, ", "))
	}
	return nil
}

// decode превращает строку из базы в значение: секрет расшифровывает, остальное проверяет по типу.
func (s *Settings) decode(def Def, raw string) (string, error) {
	if def.Secret {
		return open(s.key, def.Key, raw)
	}
	return normalize(def.Kind, raw)
}

// Set меняет настройку. Неизвестный ключ и неверный тип отклоняются, старое значение остаётся.
// Значение, равное умолчанию, в базе не хранится: строка удаляется.
func (s *Settings) Set(key, raw string) error {
	def, ok := find(key)
	if !ok {
		return fmt.Errorf("%w: %s", ErrUnknownKey, key)
	}
	if def.Secret {
		// Секрет меняется только через подтверждение владельца в Telegram (ядро зовёт SetSecret).
		return fmt.Errorf("%w: %s", ErrSecretNeedsConfirm, key)
	}
	return s.set(def, raw)
}

// SetSecret меняет секретную настройку. Его зовёт только ядро после «Да» владельца,
// поэтому случайный обработчик админки не запишет ключ кассы в обход подтверждения.
func (s *Settings) SetSecret(key, raw string) error {
	def, ok := find(key)
	if !ok || !def.Secret {
		return fmt.Errorf("%w: %s", ErrUnknownKey, key)
	}
	return s.set(def, raw)
}

func (s *Settings) set(def Def, raw string) error {
	ch, err := s.prepare(def, raw)
	if err != nil {
		return err
	}
	if ch.Delete {
		return s.Reset(def.Key)
	}
	if err := s.st.Save(ch.Key, ch.Stored, s.now().Unix()); err != nil {
		return fmt.Errorf("запись настройки %s: %w", ch.Key, err)
	}
	s.Apply(ch)
	return nil
}

// Change это подготовленная правка настройки: проверена, нормализована, секрет зашифрован. Ядро пишет её в базу
// своей транзакцией вместе с журналом (Stored под ключом Key, а Delete удаляет строку) и после коммита зовёт Apply.
type Change struct {
	Key    string
	Value  string // нормализованное значение, то, что увидит код после Apply
	Stored string // что лежит в базе: у секрета шифротекст
	Delete bool   // строку надо удалить: значение равно умолчанию или это сброс
	Secret bool
}

// PrepareSet проверяет значение и готовит запись, ничего не меняя. Секретную настройку так не меняют:
// для неё PrepareSecret.
func (s *Settings) PrepareSet(key, raw string) (Change, error) {
	def, ok := find(key)
	if !ok {
		return Change{}, fmt.Errorf("%w: %s", ErrUnknownKey, key)
	}
	if def.Secret {
		return Change{}, fmt.Errorf("%w: %s", ErrSecretNeedsConfirm, key)
	}
	return s.prepare(def, raw)
}

// PrepareSecret готовит запись секрета. Её зовёт только ядро после «Да» владельца.
func (s *Settings) PrepareSecret(key, raw string) (Change, error) {
	def, ok := find(key)
	if !ok || !def.Secret {
		return Change{}, fmt.Errorf("%w: %s", ErrUnknownKey, key)
	}
	return s.prepare(def, raw)
}

// PrepareReset готовит возврат к умолчанию.
func (s *Settings) PrepareReset(key string) (Change, error) {
	def, ok := find(key)
	if !ok {
		return Change{}, fmt.Errorf("%w: %s", ErrUnknownKey, key)
	}
	return Change{Key: key, Value: def.Default, Delete: true, Secret: def.Secret}, nil
}

func (s *Settings) prepare(def Def, raw string) (Change, error) {
	key := def.Key
	norm, err := normalize(def.Kind, raw)
	if err != nil {
		return Change{}, fmt.Errorf("%s: %w", key, err)
	}
	if check := checks[key]; check != nil {
		if err := check(norm); err != nil {
			return Change{}, fmt.Errorf("%s: %w: %w", key, ErrBadValue, err)
		}
	}
	ch := Change{Key: key, Value: norm, Secret: def.Secret}
	if norm == def.Default {
		ch.Delete = true
		return ch, nil
	}
	ch.Stored = norm
	if def.Secret {
		if ch.Stored, err = seal(s.key, key, norm); err != nil {
			return Change{}, fmt.Errorf("шифрование настройки %s: %w", key, err)
		}
	}
	return ch, nil
}

// Apply применяет подготовленную правку в памяти. Зовётся после того, как запись попала в базу.
func (s *Settings) Apply(ch Change) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if ch.Delete {
		delete(s.vals, ch.Key)
		return
	}
	s.vals[ch.Key] = ch.Value
}

// Reset удаляет строку из базы, дальше действует умолчание.
func (s *Settings) Reset(key string) error {
	if _, ok := find(key); !ok {
		return fmt.Errorf("%w: %s", ErrUnknownKey, key)
	}
	if err := s.st.Delete(key); err != nil {
		return fmt.Errorf("сброс настройки %s: %w", key, err)
	}
	s.mu.Lock()
	delete(s.vals, key)
	s.mu.Unlock()
	return nil
}

// raw отдаёт текущее значение: из базы или умолчание. Для неизвестного ключа ok равен false.
func (s *Settings) raw(key string) (string, bool) {
	def, ok := find(key)
	if !ok {
		return "", false
	}
	s.mu.RLock()
	v, set := s.vals[key]
	s.mu.RUnlock()
	if set {
		return v, true
	}
	return def.Default, true
}

// Bool отдаёт булеву настройку. Для неизвестного ключа или чужого типа отдаёт false.
func (s *Settings) Bool(key string) bool {
	v, _ := s.raw(key)
	b, _ := strconv.ParseBool(v)
	return b
}

// Int отдаёт целое число.
func (s *Settings) Int(key string) int64 {
	v, _ := s.raw(key)
	n, _ := strconv.ParseInt(v, 10, 64)
	return n
}

// IsSecret говорит, что настройка секретная: хранится зашифрованной и меняется только подтверждением владельца.
func (s *Settings) IsSecret(key string) bool {
	def, ok := find(key)
	return ok && def.Secret
}

// Money отдаёт сумму в копейках.
func (s *Settings) Money(key string) int64 { return s.Int(key) }

// String отдаёт строку. Для секретов отдаёт пустую строку, секрет берут только через Secret.
func (s *Settings) String(key string) string {
	if def, ok := find(key); ok && def.Secret {
		return ""
	}
	v, _ := s.raw(key)
	return v
}

// Secret отдаёт расшифрованный секрет для работы кода (например, запроса к кассе).
// Наружу, в админку и в журнал, его отдавать нельзя: для них есть Masked.
func (s *Settings) Secret(key string) string {
	if def, ok := find(key); !ok || !def.Secret {
		return ""
	}
	v, _ := s.raw(key)
	return v
}

// Masked отдаёт про секрет только признак «задан» и последние 4 символа.
func (s *Settings) Masked(key string) Masked {
	return mask(s.Secret(key))
}

// Duration отдаёт промежуток времени.
func (s *Settings) Duration(key string) time.Duration {
	v, _ := s.raw(key)
	d, _ := time.ParseDuration(v)
	return d
}

// Validate проверяет значение по типу настройки и её своей проверке, ничего не записывая. Нужна, чтобы отказать
// сразу, а не после «Да» владельца. Значение в текст ошибки не попадает.
func (s *Settings) Validate(key, raw string) error {
	def, ok := find(key)
	if !ok {
		return fmt.Errorf("%w: %s", ErrUnknownKey, key)
	}
	norm, err := normalize(def.Kind, raw)
	if err != nil {
		return fmt.Errorf("%s: %w", key, err)
	}
	if check := checks[key]; check != nil {
		if err := check(norm); err != nil {
			return fmt.Errorf("%s: %w: %w", key, ErrBadValue, err)
		}
	}
	return nil
}

// Current отдаёт текущее значение и признак «изменено» (в базе есть строка, а не умолчание). Секрет отдаёт
// пустое значение: для него есть Masked.
func (s *Settings) Current(key string) (value string, changed bool) {
	def, ok := find(key)
	if !ok {
		return "", false
	}
	s.mu.RLock()
	v, set := s.vals[key]
	s.mu.RUnlock()
	if def.Secret {
		return "", set
	}
	if !set {
		return def.Default, false
	}
	return v, true
}
