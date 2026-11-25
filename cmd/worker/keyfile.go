package main

import (
	"bytes"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
)

// keyFile это файл, куда rotate кладёт новый ключ, пока владелец не перенёс его в .env.
func keyFile(dbPath string) string { return dbPath + ".newkey" }

// checkKeyFile разбирается с файлом нового ключа при старте. Ключ из файла уже в .env: файл больше
// не нужен и удаляется, иначе копия папки унесла бы базу вместе с ключом. Ключ другой: смена ключа
// не закончена, база уже на новом ключе, и старт с прежним ключом отказывает с понятной причиной.
func checkKeyFile(env Env) (string, error) {
	path := keyFile(env.DBPath)
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	line := strings.TrimSpace(string(bytes.TrimPrefix(b, []byte("SECRET_KEY="))))
	key, err := base64.StdEncoding.DecodeString(line)
	if err != nil || len(key) != 32 {
		return "", fmt.Errorf("файл %s испорчен: проверьте ключ в .env и удалите файл", path)
	}
	if subtle.ConstantTimeCompare(key, env.SecretKey) != 1 {
		return "", fmt.Errorf("смена ключа не закончена: впишите SECRET_KEY из %s в .env и запустите снова", path)
	}
	if err := os.Remove(path); err != nil {
		return "", err
	}
	return "новый ключ уже в .env, файл " + path + " удалён", nil
}
