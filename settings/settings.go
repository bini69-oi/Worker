package settings

import (
	"fmt"
	"strconv"
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
	now func() time.Time

	mu   sync.RWMutex
	vals map[string]string
}

// New читает таблицу и возвращает готовые настройки. Время подставляется снаружи.
func New(st Store, now func() time.Time) (*Settings, error) {
	s := &Settings{st: st, now: now, vals: map[string]string{}}
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
	for key, raw := range rows {
		def, ok := find(key)
		if !ok {
			continue
		}
		if norm, err := normalize(def.Kind, raw); err == nil {
			vals[key] = norm
		}
	}
	s.mu.Lock()
	s.vals = vals
	s.mu.Unlock()
	return nil
}

// Set меняет настройку. Неизвестный ключ и неверный тип отклоняются, старое значение остаётся.
// Значение, равное умолчанию, в базе не хранится: строка удаляется.
func (s *Settings) Set(key, raw string) error {
	def, ok := find(key)
	if !ok {
		return fmt.Errorf("%w: %s", ErrUnknownKey, key)
	}
	norm, err := normalize(def.Kind, raw)
	if err != nil {
		return fmt.Errorf("%s: %w", key, err)
	}
	if norm == def.Default {
		return s.Reset(key)
	}
	if err := s.st.Save(key, norm, s.now().Unix()); err != nil {
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

// Money отдаёт сумму в копейках.
func (s *Settings) Money(key string) int64 { return s.Int(key) }

// String отдаёт строку.
func (s *Settings) String(key string) string {
	v, _ := s.raw(key)
	return v
}

// Duration отдаёт промежуток времени.
func (s *Settings) Duration(key string) time.Duration {
	v, _ := s.raw(key)
	d, _ := time.ParseDuration(v)
	return d
}
