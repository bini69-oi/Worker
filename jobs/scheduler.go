package jobs

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// Job это фоновая задача: имя для журнала, пауза между запусками и сама работа.
type Job struct {
	Name string
	// Every отдаёт паузу перед следующим запуском. Это функция, поэтому правка настройки действует без перезапуска.
	Every func() time.Duration
	Run   func(ctx context.Context) error
	// Wake будит задачу раньше паузы (например, ядро положило задачу в очередь). Может быть nil.
	Wake <-chan struct{}
}

// Scheduler запускает задачи. Каждая задача идёт в своей горутине и не перекрывается сама с собой:
// следующий запуск только после окончания предыдущего.
type Scheduler struct {
	Jobs []Job
	Log  *slog.Logger
	// After ждёт паузу. В тестах подменяется, чтобы не ждать по-настоящему.
	After func(d time.Duration) <-chan time.Time
}

// Run запускает все задачи и ждёт отмены ctx. После отмены дожидается задач, которые уже выполняются,
// и только потом возвращается: так остановка процесса не обрывает задачу на середине.
func (s *Scheduler) Run(ctx context.Context) {
	after := s.After
	if after == nil {
		after = time.After
	}
	var wg sync.WaitGroup
	for _, j := range s.Jobs {
		wg.Go(func() { s.loop(ctx, j, after) })
	}
	wg.Wait()
}

func (s *Scheduler) loop(ctx context.Context, j Job, after func(time.Duration) <-chan time.Time) {
	for {
		s.runOnce(ctx, j)
		select {
		case <-ctx.Done():
			return
		case <-after(j.Every()):
		case <-j.Wake:
		}
	}
}

// runOnce выполняет задачу один раз. Паника в задаче пишется в журнал и не роняет процесс.
func (s *Scheduler) runOnce(ctx context.Context, j Job) {
	if ctx.Err() != nil {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			s.Log.Error("фоновая задача упала", "job", j.Name, "panic", r)
		}
	}()
	if err := j.Run(context.WithoutCancel(ctx)); err != nil {
		s.Log.Warn("фоновая задача с ошибкой", "job", j.Name, "err", err)
	}
}
