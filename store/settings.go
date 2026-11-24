package store

import "fmt"

// SettingsStore хранит настройки в таблице settings.
type SettingsStore struct{ db *DB }

// NewSettingsStore возвращает хранилище настроек поверх базы.
func NewSettingsStore(db *DB) *SettingsStore { return &SettingsStore{db: db} }

// LoadAll отдаёт все строки таблицы.
func (s *SettingsStore) LoadAll() (map[string]string, error) {
	rows, err := s.db.R.Query("SELECT key, value FROM settings")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		out[k] = v
	}
	return out, rows.Err()
}

// Save записывает или заменяет строку.
func (s *SettingsStore) Save(key, value string, updatedAt int64) error {
	_, err := s.db.W.Exec(
		`INSERT INTO settings (key, value, updated_at) VALUES (?, ?, ?)
		 ON CONFLICT (key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
		key, value, updatedAt)
	if err != nil {
		return fmt.Errorf("settings: %w", err)
	}
	return nil
}

// Delete удаляет строку. Если её нет, это не ошибка.
func (s *SettingsStore) Delete(key string) error {
	_, err := s.db.W.Exec("DELETE FROM settings WHERE key = ?", key)
	return err
}
