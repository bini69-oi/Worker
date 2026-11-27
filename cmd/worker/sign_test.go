package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/bini69-oi/worker/release"
)

func TestSignKeygen(t *testing.T) {
	var out bytes.Buffer
	if err := signCommand([]string{"keygen"}, &out); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	i := strings.Index(text, "RELEASE_SIGNING_KEY=")
	if i < 0 {
		t.Fatalf("закрытый ключ не напечатан: %q", text)
	}
	rest := text[i+len("RELEASE_SIGNING_KEY="):]
	priv, err := release.ParsePrivate(strings.TrimSpace(strings.SplitN(rest, "\n", 2)[0]))
	if err != nil {
		t.Fatalf("напечатанный закрытый ключ не читается: %v", err)
	}
	j := strings.Index(text, "-----BEGIN PUBLIC KEY-----")
	if j < 0 {
		t.Fatalf("открытый ключ не напечатан: %q", text)
	}
	pubPEM := text[j : strings.Index(text, "-----END PUBLIC KEY-----")+len("-----END PUBLIC KEY-----")+1]
	msg := []byte("SHA256SUMS")
	if err := release.Verify([]byte(pubPEM), msg, release.Sign(priv, msg)); err != nil {
		t.Fatalf("пара не сходится: %v", err)
	}
}

func TestSignUnknown(t *testing.T) {
	if err := signCommand([]string{"x"}, &bytes.Buffer{}); err == nil {
		t.Fatal("неизвестная подкоманда должна давать ошибку")
	}
	if err := signCommand(nil, &bytes.Buffer{}); err == nil {
		t.Fatal("без подкоманды нужна ошибка")
	}
}
