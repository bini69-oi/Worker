package wata

import (
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha512"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	keyTTL        = time.Hour   // как долго верим закэшированному ключу
	keyRefetchMin = time.Minute // чаще этого ключ при неудачной проверке не перезапрашиваем
)

// keyCache хранит публичный ключ WATA, которым подписаны вебхуки.
type keyCache struct {
	key *rsa.PublicKey
	at  time.Time
}

// publicKey отдаёт ключ из кэша или грузит его у WATA. force просит свежий, но не чаще раза в минуту.
func (g *Gateway) publicKey(ctx context.Context, force bool) (*rsa.PublicKey, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	age := g.now().Sub(g.keys.at)
	if g.keys.key != nil && age < keyTTL && (!force || age < keyRefetchMin) {
		return g.keys.key, nil
	}
	req, err := g.newRequest(ctx, http.MethodGet, "/public-key", nil)
	if err != nil {
		return nil, err
	}
	resp, err := g.client.Do(req)
	if err != nil {
		return nil, errors.New("касса WATA недоступна")
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if resp.StatusCode != http.StatusOK {
		return nil, errors.New("WATA не отдала публичный ключ")
	}
	var wrap struct {
		Value string `json:"value"`
	}
	text := string(raw)
	if json.Unmarshal(raw, &wrap) == nil && wrap.Value != "" {
		text = wrap.Value
	}
	key, err := parseKey(text)
	if err != nil {
		return nil, err
	}
	g.keys = keyCache{key: key, at: g.now()}
	return key, nil
}

// parseKey читает RSA-ключ: PEM (PKIX или PKCS1) либо голый base64 без рамки.
func parseKey(text string) (*rsa.PublicKey, error) {
	text = strings.TrimSpace(text)
	der := []byte(nil)
	if block, _ := pem.Decode([]byte(text)); block != nil {
		der = block.Bytes
	} else {
		b, err := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(text), ""))
		if err != nil {
			return nil, errors.New("публичный ключ WATA не разобран")
		}
		der = b
	}
	if k, err := x509.ParsePKIXPublicKey(der); err == nil {
		if rk, ok := k.(*rsa.PublicKey); ok {
			return rk, nil
		}
		return nil, errors.New("публичный ключ WATA не RSA")
	}
	if rk, err := x509.ParsePKCS1PublicKey(der); err == nil {
		return rk, nil
	}
	return nil, errors.New("публичный ключ WATA не разобран")
}

// checkSignature сверяет RSA-подпись (PKCS1 v1.5, SHA-512, base64 в X-Signature) по сырому телу.
// Не сошлась с кэшированным ключом: один раз перезапрашиваем ключ, вдруг его сменили.
func (g *Gateway) checkSignature(ctx context.Context, body []byte, sig string) error {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(sig))
	if err != nil || len(raw) == 0 {
		return errBadSig
	}
	sum := sha512.Sum512(body)
	for _, force := range []bool{false, true} {
		key, err := g.publicKey(ctx, force)
		if err != nil {
			return err
		}
		if rsa.VerifyPKCS1v15(key, crypto.SHA512, sum[:], raw) == nil {
			return nil
		}
	}
	return errBadSig
}
