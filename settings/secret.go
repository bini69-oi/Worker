package settings

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"strings"
)

// encPrefix это версия формата. Если формат сменится, появится enc2: и старые строки останутся читаемыми.
const encPrefix = "enc1:"

var errDecrypt = errors.New("секрет не расшифровался: ключ шифрования не тот или строка повреждена")

func newGCM(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// seal шифрует AES-256-GCM со случайным nonce. Имя настройки идёт как привязанные данные (AAD),
// поэтому зашифрованное значение одной настройки нельзя подсунуть в другую.
func seal(key []byte, name, plain string) (string, error) {
	gcm, err := newGCM(key)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	out := gcm.Seal(nonce, nonce, []byte(plain), []byte(name))
	return encPrefix + base64.StdEncoding.EncodeToString(out), nil
}

// open расшифровывает строку, сделанную seal. Текст ошибки не содержит ни значения, ни ключа.
func open(key []byte, name, stored string) (string, error) {
	body, ok := strings.CutPrefix(stored, encPrefix)
	if !ok {
		return "", errDecrypt
	}
	raw, err := base64.StdEncoding.DecodeString(body)
	if err != nil {
		return "", errDecrypt
	}
	gcm, err := newGCM(key)
	if err != nil {
		return "", err
	}
	if len(raw) < gcm.NonceSize() {
		return "", errDecrypt
	}
	nonce, data := raw[:gcm.NonceSize()], raw[gcm.NonceSize():]
	plain, err := gcm.Open(nil, nonce, data, []byte(name))
	if err != nil {
		return "", errDecrypt
	}
	return string(plain), nil
}

// Masked это то, что админка видит вместо секрета: задан ли он и последние 4 символа.
type Masked struct {
	IsSet bool
	Last4 string
}

// mask показывает хвост только у длинных секретов (от 16 символов): у коротких 4 символа это уже заметная часть.
func mask(plain string) Masked {
	r := []rune(plain)
	m := Masked{IsSet: len(r) > 0}
	if len(r) >= 16 {
		m.Last4 = string(r[len(r)-4:])
	}
	return m
}
