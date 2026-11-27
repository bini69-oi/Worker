package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/bini69-oi/worker/release"
)

func TestSignFile(t *testing.T) {
	priv, pub, _ := release.GenerateKey()
	dir := t.TempDir()
	in := filepath.Join(dir, "SHA256SUMS")
	out := filepath.Join(dir, "SHA256SUMS.sig")
	sums := []byte("deadbeef  worker-linux-amd64\n")
	if err := os.WriteFile(in, sums, 0o644); err != nil {
		t.Fatal(err)
	}
	getenv := func(k string) string {
		if k == "RELEASE_SIGNING_KEY" {
			return release.EncodePrivate(priv)
		}
		return ""
	}
	if err := run([]string{in, out}, getenv); err != nil {
		t.Fatal(err)
	}
	sig, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if err := release.Verify(pub, sums, sig); err != nil {
		t.Fatalf("подпись не проверяется: %v", err)
	}
}

func TestSignNeedsKey(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "S")
	_ = os.WriteFile(in, []byte("x"), 0o644)
	if err := run([]string{in, filepath.Join(dir, "S.sig")}, func(string) string { return "" }); err == nil {
		t.Fatal("без ключа подписывать нельзя")
	}
	if _, err := os.Stat(filepath.Join(dir, "S.sig")); err == nil {
		t.Fatal("файл подписи появился без ключа")
	}
}

func TestSignUsage(t *testing.T) {
	if err := run(nil, func(string) string { return "" }); err == nil || !bytes.Contains([]byte(err.Error()), []byte("sign")) {
		t.Fatalf("нужна подсказка по запуску: %v", err)
	}
}
