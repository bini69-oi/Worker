package coretest

import (
	"sync"
	"testing"
	"time"

	"github.com/bini69-oi/worker/texts"
)

// memTexts это хранилище правок текстов в памяти.
type memTexts struct {
	mu   sync.Mutex
	rows map[[2]string]string
}

func (m *memTexts) LoadAll() ([]texts.Row, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []texts.Row
	for k, v := range m.rows {
		out = append(out, texts.Row{Key: k[0], Lang: k[1], Value: v})
	}
	return out, nil
}

func (m *memTexts) Save(key, lang, value string, _ int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rows[[2]string{key, lang}] = value
	return nil
}

func (m *memTexts) Delete(key, lang string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.rows, [2]string{key, lang})
	return nil
}

// NewTexts возвращает настоящие тексты с умолчаниями из кода поверх памяти.
func NewTexts(t *testing.T) *texts.Texts {
	t.Helper()
	tx, err := texts.New(&memTexts{rows: map[[2]string]string{}}, func() string { return "ru" }, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	return tx
}
