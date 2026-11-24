package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Task это задача из очереди.
type Task struct {
	ID        int64
	Kind      string
	Payload   []byte
	Attempts  int
	CreatedAt time.Time
}

// stuckAfter это срок, после которого о зависшей задаче узнаёт админ.
const stuckAfter = time.Hour

// backoff это пауза перед следующей попыткой: 5 с, 30 с, 2 мин, 10 мин, дальше каждые 30 мин.
func backoff(attempts int) time.Duration {
	steps := []time.Duration{5 * time.Second, 30 * time.Second, 2 * time.Minute, 10 * time.Minute}
	if attempts >= 1 && attempts <= len(steps) {
		return steps[attempts-1]
	}
	return 30 * time.Minute
}

// ProcessDue выполняет задачи, время которых пришло. Упавшая задача откладывается по backoff.
// Зовёт его один воркер: сразу после изменений (сигнал Kick) и раз в секунду.
// Возвращает, сколько задач выполнено.
func (c *Core) ProcessDue(ctx context.Context) (int, error) {
	now := c.clock.Now()
	tasks, err := c.store.DueTasks(ctx, now, 50)
	if err != nil {
		return 0, err
	}
	done := 0
	for _, t := range tasks {
		if err := c.runTask(ctx, t); err != nil {
			attempts := t.Attempts + 1
			if ferr := c.store.TaskFailed(ctx, t.ID, attempts, now.Add(backoff(attempts)), shortErr(err)); ferr != nil {
				return done, ferr
			}
			continue
		}
		if err := c.store.TaskDone(ctx, t.ID, now); err != nil {
			return done, err
		}
		done++
	}
	return done, c.alertStuck(ctx, now)
}

func (c *Core) runTask(ctx context.Context, t Task) error {
	switch t.Kind {
	case KindPanelSync:
		return runPanelSync(ctx, c, t.Payload)
	case KindNotifyUser:
		var n NotifyTask
		if err := json.Unmarshal(t.Payload, &n); err != nil {
			return err
		}
		return c.notifier.User(ctx, n)
	case KindNotifyAdmin:
		var n AdminTask
		if err := json.Unmarshal(t.Payload, &n); err != nil {
			return err
		}
		return c.notifier.Admin(ctx, n)
	}
	return fmt.Errorf("неизвестный вид задачи %q", t.Kind)
}

// alertStuck один раз сообщает админу о задачах, которые не проходят дольше часа.
func (c *Core) alertStuck(ctx context.Context, now time.Time) error {
	stuck, err := c.store.StuckTasks(ctx, now.Add(-stuckAfter))
	if err != nil || len(stuck) == 0 {
		return err
	}
	count := map[string]int{}
	ids := make([]int64, 0, len(stuck))
	for _, t := range stuck {
		count[t.Kind]++
		ids = append(ids, t.ID)
	}
	kinds := make([]string, 0, len(count))
	for k, n := range count {
		kinds = append(kinds, k+" x"+strconv.Itoa(n))
	}
	sort.Strings(kinds)
	return c.tx(ctx, func(tx Tx) error {
		task := AdminTask{Topic: TopicErrors, Key: "admin.tasks_stuck",
			Vars: map[string]string{"count": strconv.Itoa(len(stuck)), "kinds": strings.Join(kinds, ", ")}}
		if err := enqueueAdmin(tx, task, now); err != nil {
			return err
		}
		return tx.MarkAlerted(ids)
	})
}

// shortErr обрезает текст ошибки для очереди: это подсказка админу, а не журнал.
func shortErr(err error) string {
	s := err.Error()
	if len(s) > 300 {
		s = s[:300]
	}
	return s
}

// errNoNotifier значит, что уведомлять пока некому: бот не подключён.
var errNoNotifier = errors.New("уведомления не подключены")

type noNotifier struct{}

func (noNotifier) User(context.Context, NotifyTask) error { return errNoNotifier }
func (noNotifier) Admin(context.Context, AdminTask) error { return errNoNotifier }
