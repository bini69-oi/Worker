package jobs

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bini69-oi/worker/core"
	"github.com/bini69-oi/worker/core/coretest"
)

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

// ticks это управляемые паузы: тест сам решает, когда пауза кончилась.
type ticks struct {
	mu sync.Mutex
	ch map[time.Duration]chan time.Time
}

func newTicks() *ticks { return &ticks{ch: map[time.Duration]chan time.Time{}} }

func (t *ticks) get(d time.Duration) chan time.Time {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.ch[d] == nil {
		t.ch[d] = make(chan time.Time)
	}
	return t.ch[d]
}

func (t *ticks) after(d time.Duration) <-chan time.Time { return t.get(d) }

// fire заканчивает одну паузу длиной d.
func (t *ticks) fire(d time.Duration) { t.get(d) <- time.Time{} }

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("не дождались")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestJobsRunOnSchedule(t *testing.T) {
	tk := newTicks()
	var fast, slow atomic.Int32
	s := &Scheduler{Log: quiet, After: tk.after, Jobs: []Job{
		{Name: "fast", Every: fixed(time.Second), Run: func(context.Context) error { fast.Add(1); return nil }},
		{Name: "slow", Every: fixed(time.Hour), Run: func(context.Context) error { slow.Add(1); return nil }},
	}}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { s.Run(ctx); close(done) }()

	waitFor(t, func() bool { return fast.Load() == 1 && slow.Load() == 1 }) // первый запуск сразу
	tk.fire(time.Second)
	tk.fire(time.Second)
	waitFor(t, func() bool { return fast.Load() == 3 })
	if slow.Load() != 1 {
		t.Fatalf("медленная задача запустилась раньше своей паузы: %d", slow.Load())
	}
	cancel()
	<-done
}

func TestWakeRunsEarly(t *testing.T) {
	tk := newTicks()
	wake := make(chan struct{}, 1)
	var runs atomic.Int32
	s := &Scheduler{Log: quiet, After: tk.after, Jobs: []Job{
		{Name: "queue", Every: fixed(time.Hour), Wake: wake, Run: func(context.Context) error { runs.Add(1); return nil }},
	}}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { s.Run(ctx); close(done) }()
	waitFor(t, func() bool { return runs.Load() == 1 })
	wake <- struct{}{}
	waitFor(t, func() bool { return runs.Load() == 2 })
	cancel()
	<-done
}

func TestStopWaitsForRunningJob(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	var finished atomic.Bool
	s := &Scheduler{Log: quiet, Jobs: []Job{{Name: "long", Every: fixed(time.Hour), Run: func(ctx context.Context) error {
		close(started)
		<-release
		if ctx.Err() != nil {
			return errors.New("задаче оборвали контекст")
		}
		finished.Store(true)
		return nil
	}}}}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { s.Run(ctx); close(done) }()
	<-started
	cancel()
	select {
	case <-done:
		t.Fatal("планировщик вернулся, не дождавшись задачи")
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	<-done
	if !finished.Load() {
		t.Fatal("задача не доработала до конца")
	}
}

func TestPanicDoesNotStop(t *testing.T) {
	tk := newTicks()
	var runs atomic.Int32
	s := &Scheduler{Log: quiet, After: tk.after, Jobs: []Job{{Name: "boom", Every: fixed(time.Second), Run: func(context.Context) error {
		runs.Add(1)
		panic("сломалось")
	}}}}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { s.Run(ctx); close(done) }()
	waitFor(t, func() bool { return runs.Load() == 1 })
	tk.fire(time.Second)
	waitFor(t, func() bool { return runs.Load() == 2 })
	cancel()
	<-done
}

func TestCoreJobsList(t *testing.T) {
	cfg := coretest.NewConfig(t)
	c := core.New(core.Deps{Store: coretest.NewStore(), Config: cfg, Panel: coretest.NewPanel()})
	jobs := CoreJobs(c, cfg)
	want := map[string]time.Duration{"queue": time.Second, "expire": time.Minute, "remind": 10 * time.Minute,
		"autorenew": 10 * time.Minute, "broadcast": time.Second, "summary": time.Minute, "cabinet": time.Hour, "payments": time.Minute, "reconcile": 60 * time.Minute, "support": 24 * time.Hour}
	if len(jobs) != len(want) {
		t.Fatalf("задач %d, ждали %d", len(jobs), len(want))
	}
	for _, j := range jobs {
		if want[j.Name] != j.Every() {
			t.Errorf("%s: пауза %v, ждали %v", j.Name, j.Every(), want[j.Name])
		}
		if err := j.Run(context.Background()); err != nil {
			t.Errorf("%s на пустой базе: %v", j.Name, err)
		}
	}
	if err := cfg.Set("panel.reconcile_minutes", "5"); err != nil {
		t.Fatal(err)
	}
	for _, j := range jobs {
		if j.Name == "reconcile" && j.Every() != 5*time.Minute {
			t.Fatalf("правка настройки не изменила паузу сверки: %v", j.Every())
		}
	}
}

type fakeBackuper struct{ n atomic.Int32 }

func (f *fakeBackuper) RunDue(context.Context) error { f.n.Add(1); return nil }

func TestBackupJobRunsHourly(t *testing.T) {
	b := &fakeBackuper{}
	tk := newTicks()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	s := &Scheduler{Jobs: []Job{BackupJob(b)}, Log: quiet, After: tk.after}
	go func() { s.Run(ctx); close(done) }()

	waitFor(t, func() bool { return b.n.Load() == 1 }) // сразу при старте
	tk.fire(time.Hour)
	waitFor(t, func() bool { return b.n.Load() == 2 })
	cancel()
	<-done
}
