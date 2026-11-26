package tg

import (
	"sync"
	"testing"
)

func TestDedupTwice(t *testing.T) {
	d := NewDedup(0)
	if d.Seen(5) {
		t.Fatal("первый раз не повтор")
	}
	if !d.Seen(5) {
		t.Fatal("второй раз повтор")
	}
}

func TestDedupRingForgetsOld(t *testing.T) {
	d := NewDedup(3)
	for i := int64(1); i <= 3; i++ {
		d.Seen(i)
	}
	d.Seen(4) // вытесняет 1
	if d.Seen(1) {
		t.Fatal("1 уже забыт")
	}
	// теперь вытеснена 2
	if !d.Seen(4) || !d.Seen(3) {
		t.Fatal("4 и 3 ещё помним")
	}
}

func TestDedupConcurrent(t *testing.T) {
	d := NewDedup(DedupSize)
	var wg sync.WaitGroup
	var mu sync.Mutex
	fresh := 0
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := int64(0); i < 1000; i++ {
				if !d.Seen(i) {
					mu.Lock()
					fresh++
					mu.Unlock()
				}
			}
		}()
	}
	wg.Wait()
	if fresh != 1000 {
		t.Fatalf("новых %d, ждали 1000", fresh)
	}
}
