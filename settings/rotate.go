package settings

import (
	"crypto/rand"
	"errors"
	"fmt"
)

// NewKey делает новый мастер-ключ: 32 случайных байта.
func NewKey() []byte {
	k := make([]byte, 32)
	_, _ = rand.Read(k) // rand.Read в Go не возвращает ошибку
	return k
}

// Reencrypt перешифровывает секреты из строк базы старым ключом в новый. Обычные настройки не меняются.
// Если хоть один секрет не расшифровался старым ключом, отказ целиком: ключ не тот.
func Reencrypt(rows map[string]string, oldKey, newKey []byte) (map[string]string, error) {
	if len(oldKey) != 32 || len(newKey) != 32 {
		return nil, errors.New("ключ шифрования должен быть 32 байта")
	}
	out := make(map[string]string, len(rows))
	for key, stored := range rows {
		def, ok := find(key)
		if !ok || !def.Secret {
			out[key] = stored
			continue
		}
		plain, err := open(oldKey, key, stored)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", key, err)
		}
		if out[key], err = seal(newKey, key, plain); err != nil {
			return nil, err
		}
	}
	return out, nil
}
