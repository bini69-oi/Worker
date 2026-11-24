package settings

import (
	"errors"
	"sync"
)

// fakeStore это хранилище в памяти для тестов.
type fakeStore struct {
	mu   sync.Mutex
	rows map[string]string
	fail error
}

func newFake() *fakeStore { return &fakeStore{rows: map[string]string{}} }

func (f *fakeStore) LoadAll() (map[string]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail != nil {
		return nil, f.fail
	}
	out := make(map[string]string, len(f.rows))
	for k, v := range f.rows {
		out[k] = v
	}
	return out, nil
}

func (f *fakeStore) Save(key, value string, _ int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail != nil {
		return f.fail
	}
	f.rows[key] = value
	return nil
}

func (f *fakeStore) Delete(key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail != nil {
		return f.fail
	}
	delete(f.rows, key)
	return nil
}

func (f *fakeStore) has(key string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, ok := f.rows[key]
	return ok
}

var errFake = errors.New("сбой хранилища")
