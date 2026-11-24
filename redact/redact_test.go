package redact

import (
	"bytes"
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
