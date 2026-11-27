// Пакет release подписывает и проверяет релизы: список сумм SHA256SUMS подписан Ed25519 ключом проекта.
// Только стандартная библиотека. Открытый ключ лежит в PEM (его читает и openssl в install.sh), закрытый
// это 32 байта в base64, их владелец кладёт в секрет репозитория.
package release

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
)

var (
	// ErrNoKey значит, что в сборке нет настоящего открытого ключа (стоит заглушка): проверять нечем, релиз не принимается.
	ErrNoKey = errors.New("ключ подписи релизов не задан в этой сборке")
	// ErrBadSignature значит, что подпись не сошлась со списком.
	ErrBadSignature = errors.New("подпись релиза неверна")
)

// GenerateKey делает пару ключей. Открытый ключ отдаётся в PEM.
func GenerateKey() (ed25519.PrivateKey, []byte, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		return nil, nil, err
	}
	return priv, pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}), nil
}

// EncodePrivate записывает закрытый ключ строкой base64 (32 байта seed).
func EncodePrivate(priv ed25519.PrivateKey) string {
	return base64.StdEncoding.EncodeToString(priv.Seed())
}

// ParsePrivate читает закрытый ключ из строки base64. Текст ошибки не содержит значения.
func ParsePrivate(s string) (ed25519.PrivateKey, error) {
	seed, err := base64.StdEncoding.DecodeString(s)
	if err != nil || len(seed) != ed25519.SeedSize {
		return nil, errors.New("закрытый ключ подписи: нужно 32 байта в base64")
	}
	return ed25519.NewKeyFromSeed(seed), nil
}

// Sign подписывает сообщение. Подпись это 64 байта без обёрток.
func Sign(priv ed25519.PrivateKey, msg []byte) []byte { return ed25519.Sign(priv, msg) }

// ParsePublic читает открытый ключ из PEM. В файле нет блока PEM (заглушка): ErrNoKey.
func ParsePublic(keyPEM []byte) (ed25519.PublicKey, error) {
	block, _ := pem.Decode(keyPEM)
	if block == nil {
		return nil, ErrNoKey
	}
	k, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, errors.New("открытый ключ подписи испорчен")
	}
	pub, ok := k.(ed25519.PublicKey)
	if !ok {
		return nil, errors.New("открытый ключ подписи не Ed25519")
	}
	return pub, nil
}

// Verify проверяет подпись msg открытым ключом в PEM.
func Verify(keyPEM, msg, sig []byte) error {
	pub, err := ParsePublic(keyPEM)
	if err != nil {
		return err
	}
	if len(sig) != ed25519.SignatureSize || !ed25519.Verify(pub, msg, sig) {
		return ErrBadSignature
	}
	return nil
}
