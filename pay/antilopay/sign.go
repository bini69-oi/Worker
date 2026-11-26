package antilopay

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"strings"
)

// derOf достаёт байты ключа из PEM или из голого base64 без рамки.
func derOf(text string) ([]byte, error) {
	text = strings.TrimSpace(text)
	if block, _ := pem.Decode([]byte(text)); block != nil {
		return block.Bytes, nil
	}
	return base64.StdEncoding.DecodeString(strings.Join(strings.Fields(text), ""))
}

// parsePrivate читает закрытый RSA-ключ магазина: PKCS8 или PKCS1.
func parsePrivate(text string) (*rsa.PrivateKey, error) {
	bad := errors.New("закрытый ключ Antilopay не разобран")
	der, err := derOf(text)
	if err != nil {
		return nil, bad
	}
	if k, err := x509.ParsePKCS8PrivateKey(der); err == nil {
		if rk, ok := k.(*rsa.PrivateKey); ok {
			return rk, nil
		}
		return nil, errors.New("закрытый ключ Antilopay не RSA")
	}
	if rk, err := x509.ParsePKCS1PrivateKey(der); err == nil {
		return rk, nil
	}
	return nil, bad
}

// parsePublic читает открытый RSA-ключ кассы: PKIX, PKCS1 или сертификат.
func parsePublic(text string) (*rsa.PublicKey, error) {
	bad := errors.New("открытый ключ Antilopay не разобран")
	der, err := derOf(text)
	if err != nil {
		return nil, bad
	}
	if k, err := x509.ParsePKIXPublicKey(der); err == nil {
		if rk, ok := k.(*rsa.PublicKey); ok {
			return rk, nil
		}
		return nil, errors.New("открытый ключ Antilopay не RSA")
	}
	if rk, err := x509.ParsePKCS1PublicKey(der); err == nil {
		return rk, nil
	}
	return nil, bad
}

// signBody подписывает точные байты тела RSA PKCS1 v1.5 SHA-256, результат base64.
func signBody(keyText string, body []byte) (string, error) {
	key, err := parsePrivate(keyText)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(body)
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, sum[:])
	if err != nil {
		return "", errors.New("запрос к Antilopay не подписан")
	}
	return base64.StdEncoding.EncodeToString(sig), nil
}

// checkSignature сверяет подпись вебхука открытым ключом кассы по точным байтам тела.
func checkSignature(keyText string, body []byte, sig string) error {
	key, err := parsePublic(keyText)
	if err != nil {
		return err
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(sig))
	if err != nil || len(raw) == 0 {
		return errBadSig
	}
	sum := sha256.Sum256(body)
	if rsa.VerifyPKCS1v15(key, crypto.SHA256, sum[:], raw) != nil {
		return errBadSig
	}
	return nil
}
