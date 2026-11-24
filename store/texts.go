package store

import (
	"fmt"

	"github.com/bini69-oi/worker/texts"
)

// TextsStore хранит правки текстов в таблице texts.
type TextsStore struct{ db *DB }

// NewTextsStore возвращает хранилище текстов поверх базы.
func NewTextsStore(db *DB) *TextsStore { return &TextsStore{db: db} }

// LoadAll отдаёт все правки.
func (s *TextsStore) LoadAll() ([]texts.Row, error) {
	rows, err := s.db.R.Query("SELECT key, lang, value FROM texts")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []texts.Row
	for rows.Next() {
		var r texts.Row
		if err := rows.Scan(&r.Key, &r.Lang, &r.Value); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// Save записывает или заменяет правку.
func (s *TextsStore) Save(key, lang, value string, updatedAt int64) error {
	_, err := s.db.W.Exec(
		`INSERT INTO texts (key, lang, value, updated_at) VALUES (?, ?, ?, ?)
		 ON CONFLICT (key, lang) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
		key, lang, value, updatedAt)
	if err != nil {
		return fmt.Errorf("texts: %w", err)
	}
	return nil
}

// Delete удаляет правку. Если её нет, это не ошибка.
func (s *TextsStore) Delete(key, lang string) error {
	_, err := s.db.W.Exec("DELETE FROM texts WHERE key = ? AND lang = ?", key, lang)
	return err
}
