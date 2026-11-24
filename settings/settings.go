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
	key := def.Key
	norm, err := normalize(def.Kind, raw)
	if err != nil {
		return fmt.Errorf("%s: %w", key, err)
	}
	if norm == def.Default {
		return s.Reset(key)
	}
	stored := norm
	if def.Secret {
		if stored, err = seal(s.key, key, norm); err != nil {
			return fmt.Errorf("шифрование настройки %s: %w", key, err)
		}
	}
	if err := s.st.Save(key, stored, s.now().Unix()); err != nil {
		return fmt.Errorf("запись настройки %s: %w", key, err)
	}
	s.mu.Lock()
	s.vals[key] = norm
	s.mu.Unlock()
	return nil
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
