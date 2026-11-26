package bot

import (
	"sync"
	"time"
)

// flood это ведро токенов на человека: rate действий в секунду, не больше burst подряд.
// Живёт в памяти: процесс один, после перезапуска счётчик флуда не нужен.
type flood struct {
	rate  float64
	burst float64
	idle  time.Duration // ведро без движения дольше idle удаляется

	mu      sync.Mutex
	buckets map[int64]*bucket
	sweep   time.Time
}

type bucket struct {
	tokens float64
	at     time.Time
	warned bool // предупреждение «слишком часто» уже показано
}

func newFlood(rate, burst float64) *flood {
	return &flood{rate: rate, burst: burst, idle: 10 * time.Minute, buckets: map[int64]*bucket{}}
}

// allow засчитывает действие. ok: пропустить. warn: показать предупреждение (один раз на серию лишних).
func (f *flood) allow(id int64, now time.Time) (ok, warn bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if now.Sub(f.sweep) > f.idle {
		for k, b := range f.buckets {
			if now.Sub(b.at) > f.idle {
				delete(f.buckets, k)
			}
		}
		f.sweep = now
	}
	b, found := f.buckets[id]
	if !found {
		b = &bucket{tokens: f.burst, at: now}
		f.buckets[id] = b
	}
	b.tokens = min(f.burst, b.tokens+now.Sub(b.at).Seconds()*f.rate)
	b.at = now
	if b.tokens >= 1 {
		b.tokens--
		b.warned = false
		return true, false
	}
	if b.warned {
		return false, false
	}
	b.warned = true
	return false, true
}

// waitKind это то, какой текст бот ждёт от абонента.
type waitKind string

const (
	waitPromo  waitKind = "promo"
	waitAmount waitKind = "amount"
)

// waitTTL это сколько бот ждёт текст после нажатия кнопки.
const waitTTL = 10 * time.Minute

// waits хранит, кто какой текст должен прислать. Любая кнопка или команда сбрасывает ожидание,
// поэтому случайный текст не становится промокодом или суммой.
type waits struct {
	mu sync.Mutex
	m  map[int64]wait
}

type wait struct {
	kind  waitKind
	until time.Time
}

func newWaits() *waits { return &waits{m: map[int64]wait{}} }

func (w *waits) set(id int64, kind waitKind, now time.Time) {
	w.mu.Lock()
	defer w.mu.Unlock()
	for k, v := range w.m {
		if now.After(v.until) {
			delete(w.m, k)
		}
	}
	w.m[id] = wait{kind: kind, until: now.Add(waitTTL)}
}

// take отдаёт и снимает ожидание. Истёкшее считается снятым.
func (w *waits) take(id int64, now time.Time) (wait, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	v, ok := w.m[id]
	delete(w.m, id)
	if !ok || now.After(v.until) {
		return wait{}, false
	}
	return v, true
}

func (w *waits) clear(id int64) {
	w.mu.Lock()
	defer w.mu.Unlock()
	delete(w.m, id)
}
