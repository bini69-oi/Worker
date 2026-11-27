package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
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

// fakeTelegram отвечает «ок» на любой метод Bot API и запоминает, какие методы звали.
func fakeTelegram(t *testing.T) (*httptest.Server, func() string) {
	var mu sync.Mutex
	var calls []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls = append(calls, r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:])
		mu.Unlock()
		if strings.HasSuffix(r.URL.Path, "/getMe") {
			_, _ = w.Write([]byte(`{"ok":true,"result":{"id":1,"is_bot":true,"username":"workerbot"}}`))
			return
		}
		if strings.HasSuffix(r.URL.Path, "/getUpdates") {
			_, _ = w.Write([]byte(`{"ok":true,"result":[]}`))
			return
		}
		_, _ = w.Write([]byte(`{"ok":true,"result":true}`))
	}))
	t.Cleanup(srv.Close)
	return srv, func() string {
		mu.Lock()
		defer mu.Unlock()
		return strings.Join(calls, ",")
	}
}

func TestServeAndStop(t *testing.T) {
	tgSrv, tgCalls := fakeTelegram(t)
	env := Env{
		BotToken: "123456:secret-bot-token", OwnerID: 1, PublicURL: "https://bot.example.com",
		PanelURL: "http://127.0.0.1:1", PanelToken: "secret-panel-token",
		SecretKey: settings.NewKey(), DBPath: filepath.Join(t.TempDir(), "w.db"), TelegramURL: tgSrv.URL,
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
	check("POST", "/tg/webhook", 401) // без секрета Telegram
	check("GET", "/dl/happ/garbage", 404)
	check("POST", "/pay/unknown", 404)
	check("POST", "/pay/stars", 401)
	for _, name := range []string{"heleket", "cryptomus", "yoomoney", "freekassa", "wata", "robokassa", "lava", "cloudpayments", "paypalych", "tribute",
		"mulenpay", "antilopay", "severpay", "rollypay", "aurapay", "cispay", "kassaai", "riopay", "paypear", "overpay",
		"etoplatezhi", "jupiter", "tabpay", "paritypay", "donut", "tbank", "stripe"} {
		check("POST", "/pay/"+name, 401) // касса подключена: поддельный вебхук отклонён, а не 404
	}
	check("POST", "/panel/webhook", 401)
	// Чужой адрес через наш прокси: уведомление ЮKassa отклоняется ещё до запроса к её API.
	req, _ := http.NewRequest("POST", base+"/pay/yookassa", strings.NewReader(`{"event":"payment.succeeded","object":{"id":"x"}}`))
	req.Header.Set("X-Real-IP", "203.0.113.9")
	if resp, err := http.DefaultClient.Do(req); err != nil || resp.StatusCode != 401 {
		t.Fatalf("уведомление ЮKassa с чужого адреса: %v %v", resp, err)
	}
	for range hookLimit {
		req, _ := http.NewRequest("POST", base+"/pay/unknown", strings.NewReader("{}"))
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
	}
	check("POST", "/pay/unknown", 429)
	check("GET", "/healthz", 200)

	for !strings.Contains(tgCalls(), "setWebhook") {
		time.Sleep(10 * time.Millisecond) // бот ставит вебхук и команды при старте
	}
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

func TestTelegramRetry(t *testing.T) {
	fails := 2
	tg := &telegram{log: slog.New(slog.NewTextHandler(io.Discard, nil)), retryWait: time.Millisecond}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	start := time.Now()
	n := 0
	tg.retry(ctx, "test", func() error {
		n++
		if n <= fails {
			return errors.New("нет сети")
		}
		return nil
	})
	if n != 3 || time.Since(start) > 5*time.Second {
		t.Fatalf("попыток %d, ждали 3", n)
	}
}

// Ежедневный бэкап подключён к процессу: после старта в папке рядом с базой появляется файл.
func TestServeMakesBackup(t *testing.T) {
	tgSrv, _ := fakeTelegram(t)
	env := Env{
		BotToken: "123456:secret-bot-token", OwnerID: 1, PublicURL: "https://bot.example.com",
		PanelURL: "http://127.0.0.1:1", PanelToken: "secret-panel-token",
		SecretKey: settings.NewKey(), DBPath: filepath.Join(t.TempDir(), "w.db"), TelegramURL: tgSrv.URL,
	}
	a, err := build(env, slog.NewJSONHandler(io.Discard, nil))
	if err != nil {
		t.Fatal(err)
	}
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- a.serve(ctx, ln) }()

	deadline := time.Now().Add(10 * time.Second)
	for {
		files, _ := filepath.Glob(filepath.Join(filepath.Dir(env.DBPath), "backups", "worker-*.db.enc"))
		if len(files) == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("ежедневный бэкап не сделан после старта")
		}
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
