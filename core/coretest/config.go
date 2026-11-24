package coretest

import (
	"sync"
	"testing"
	"time"

	"github.com/bini69-oi/worker/settings"
)

// memSettings это хранилище строк настроек в памяти.
type memSettings struct {
	mu   sync.Mutex
	rows map[string]string
}

func (m *memSettings) LoadAll() (map[string]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make(map[string]string, len(m.rows))
	for k, v := range m.rows {
		out[k] = v
	}
	return out, nil
}

func (m *memSettings) Save(key, value string, _ int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rows[key] = value
	return nil
}

func (m *memSettings) Delete(key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.rows, key)
	return nil
}

// NewConfig возвращает настоящие настройки с умолчаниями из реестра поверх памяти.
// Тест меняет их через Set, как это делает админка.
func NewConfig(t *testing.T) *settings.Settings {
	t.Helper()
	key := make([]byte, 32)
	s, err := settings.New(&memSettings{rows: map[string]string{}}, key, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	return s
}
