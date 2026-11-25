package redact

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"
)

const kassaKey = "live_Sup3rSecretKassaKey"

func TestLogHasNoSecrets(t *testing.T) {
	var buf bytes.Buffer
	set := &Set{}
	set.Add(kassaKey, "123456:BOT-TOKEN-xyz", "", "ru")
	log := slog.New(Handler(slog.NewJSONHandler(&buf, nil), set))

	kassaErr := fmt.Errorf("касса ответила 401 на ключ %s", kassaKey)
	log.Error("касса упала: "+kassaKey, "err", kassaErr, "token", "123456:BOT-TOKEN-xyz",
		slog.Group("req", "auth", "Bearer "+kassaKey), "count", 3)
	log.With("key", kassaKey).Info("готово")

	out := buf.String()
	if strings.Contains(out, kassaKey) || strings.Contains(out, "BOT-TOKEN") {
		t.Fatalf("секрет попал в журнал:\n%s", out)
	}
	if !strings.Contains(out, Mask) || !strings.Contains(out, `"count":3`) {
		t.Fatalf("журнал испорчен сверх секретов:\n%s", out)
	}
}

func TestShortValuesIgnored(t *testing.T) {
	set := &Set{}
	set.Add("ru", "12345")
	if got := set.String("lang ru, id 12345"); got != "lang ru, id 12345" {
		t.Fatalf("короткие значения не должны заменяться: %q", got)
	}
}

func TestErrorText(t *testing.T) {
	set := &Set{}
	set.Add(kassaKey)
	err := errors.New("bad key " + kassaKey)
	if got := set.String(err.Error()); strings.Contains(got, kassaKey) {
		t.Fatalf("ключ остался в тексте ошибки: %q", got)
	}
}

func TestEncodedFormsHidden(t *testing.T) {
	set := &Set{}
	set.Add("123456:AAH/x+y=token")
	for _, s := range []string{
		"GET /bot123456%3AAAH%2Fx%2By%3Dtoken/getMe",
		`{"token":"123456:AAH\/x+y=token"}`,
	} {
		if got := set.String(s); strings.Contains(got, "AAH") {
			t.Errorf("секрет в закодированном виде остался: %q", got)
		}
	}
}

func TestBase64AndHexHidden(t *testing.T) {
	set := &Set{}
	set.Add("shop1:live_secret_key")
	basic := "Authorization: Basic " + base64.StdEncoding.EncodeToString([]byte("shop1:live_secret_key"))
	hexed := hex.EncodeToString([]byte("shop1:live_secret_key"))
	for _, s := range []string{basic, "dump " + hexed} {
		if got := set.String(s); !strings.Contains(got, Mask) || strings.Contains(got, hexed) {
			t.Errorf("секрет в кодировке остался: %q", got)
		}
	}
}

func TestLongerSecretFirst(t *testing.T) {
	set := &Set{}
	set.Add("abcdef", "abcdef-and-more-secret")
	if got := set.String("x abcdef-and-more-secret y"); got != "x *** y" {
		t.Fatalf("длинный секрет открылся по частям: %q", got)
	}
}
