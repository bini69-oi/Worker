package tg

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestPoll(t *testing.T) {
	var mu sync.Mutex
	var offsets []int64
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var req struct{ Offset int64 }
		_ = json.Unmarshal(b, &req)
		mu.Lock()
		calls++
		n := calls
		offsets = append(offsets, req.Offset)
		mu.Unlock()
		switch n {
		case 1: // ошибка, клиент должен сделать паузу и продолжить
			w.WriteHeader(500)
			_, _ = w.Write([]byte(`{"ok":false,"error_code":500,"description":"oops"}`))
		case 2:
			_, _ = w.Write([]byte(`{"ok":true,"result":[{"update_id":10},{"update_id":11}]}`))
		case 3:
			_, _ = w.Write([]byte(`{"ok":true,"result":[{"update_id":12}]}`))
		default:
			<-r.Context().Done()
		}
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var slept []time.Duration
	c := newTestClient(srv)
	c.Sleep = func(_ context.Context, d time.Duration) error {
		slept = append(slept, d)
		return nil
	}
	out := make(chan Update, 10)
	done := make(chan error, 1)
	go func() { done <- Poll(ctx, c, out) }()

	for _, want := range []int64{10, 11, 12} {
		select {
		case u := <-out:
			if u.UpdateID != want {
				t.Fatalf("update_id %d, ждали %d", u.UpdateID, want)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("обновление не пришло")
		}
	}
	cancel()
	select {
	case err := <-done:
		if err != context.Canceled {
			t.Fatalf("err=%v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Poll не вышел по ctx")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(slept) != 1 || slept[0] != pollPause {
		t.Fatalf("паузы %v", slept)
	}
	if len(offsets) < 3 || offsets[0] != 0 || offsets[1] != 0 || offsets[2] != 12 {
		t.Fatalf("offset %v", offsets)
	}
}

func TestPollStopsOnFullOut(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true,"result":[{"update_id":1},{"update_id":2}]}`))
	}))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if err := Poll(ctx, newTestClient(srv), make(chan Update)); err == nil {
		t.Fatal("ждали ошибку ctx")
	}
}
