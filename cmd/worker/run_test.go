package main

import (
	"bytes"
	"context"
	"log/slog"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bini69-oi/worker/settings"
)

// syncBuf это буфер журнала, безопасный для записи из разных горутин.
type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func TestServeAndStop(t *testing.T) {
	env := Env{
		BotToken: "123456:secret-bot-token", OwnerID: 1, PublicURL: "https://bot.example.com",
		PanelURL: "http://127.0.0.1:1", PanelToken: "secret-panel-token",
		SecretKey: settings.NewKey(), DBPath: filepath.Join(t.TempDir(), "w.db"),
	}
	var logs syncBuf
	a, err := build(env, slog.NewJSONHandler(&logs, nil))
	if err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- a.serve(ctx, ln) }()

	base := "http://" + ln.Addr().String()
	check := func(method, path string, want int) {
		t.Helper()
		req, _ := http.NewRequest(method, base+path, strings.NewReader("{}"))
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != want {
			t.Fatalf("%s %s: код %d, ждали %d", method, path, resp.StatusCode, want)
		}
	}
	check("GET", "/healthz", 200)
	check("POST", "/pay/unknown", 404)
	check("POST", "/pay/stars", 401)
	check("POST", "/panel/webhook", 401)

	a.log.Warn("проверка фильтра", "token", env.BotToken, "panel", env.PanelToken)
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("процесс не остановился")
	}
	if out := logs.String(); strings.Contains(out, "secret-bot-token") || strings.Contains(out, "secret-panel-token") {
		t.Fatalf("секрет попал в журнал:\n%s", out)
	}
}
