package jobs

import (
	"context"
	"time"

	"github.com/bini69-oi/worker/core"
)

// Config это настройки, из которых берутся паузы.
type Config interface {
	Int(key string) int64
}

func fixed(d time.Duration) func() time.Duration { return func() time.Duration { return d } }

// minutes берёт паузу в минутах из настройки, но не меньше минуты.
func minutes(cfg Config, key string) func() time.Duration {
	return func() time.Duration {
		return time.Duration(max(cfg.Int(key), 1)) * time.Minute
	}
}

// count отбрасывает число из сценария ядра: планировщику нужна только ошибка.
func count(fn func(context.Context) (int, error)) func(context.Context) error {
	return func(ctx context.Context) error {
		_, err := fn(ctx)
		return err
	}
}

// CoreJobs это фоновые задачи ядра, по одной на сценарий.
func CoreJobs(c *core.Core, cfg Config) []Job {
	return []Job{
		{Name: "queue", Every: fixed(time.Second), Run: count(c.ProcessDue), Wake: c.Kick()},
		{Name: "expire", Every: fixed(time.Minute), Run: count(c.ExpireDue)},
		{Name: "remind", Every: fixed(10 * time.Minute), Run: count(c.RemindDue)},
		{Name: "autorenew", Every: fixed(10 * time.Minute), Run: count(c.AutoRenewDue)},
		// Рассылка: ядро само держит предел 25 в секунду, задача лишь будит его раз в секунду.
		{Name: "broadcast", Every: fixed(time.Second), Run: count(c.RunBroadcast)},
		{Name: "summary", Every: fixed(time.Minute), Run: count(c.SendSummary)},
		{Name: "payments", Every: fixed(time.Minute), Run: count(c.ReconcilePayments)},
		{Name: "support", Every: fixed(24 * time.Hour), Run: count(c.CleanSupport)},
		{Name: "reconcile", Every: minutes(cfg, "panel.reconcile_minutes"), Run: count(c.Reconcile)},
	}
}
