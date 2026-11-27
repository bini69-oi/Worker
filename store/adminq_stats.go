package store

import (
	"context"
	"time"

	"github.com/bini69-oi/worker/core"
)

// maxStuckUsers это сколько абонентов зависших задач сводка показывает ссылками.
const maxStuckUsers = 20

func (s *CoreStore) StatsData(ctx context.Context, q core.StatsQuery) (core.StatsData, error) {
	d := core.StatsData{Paid: map[int64]map[string]int64{}, Refunded: map[int64]map[string]int64{}}
	from, to, off := q.From.Unix(), q.To.Unix(), q.OffsetSec
	sums := func(into map[int64]map[string]int64, query string, args ...any) error {
		rows, err := s.db.R.QueryContext(ctx, query, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var day, sum int64
			var gw string
			if err := rows.Scan(&day, &gw, &sum); err != nil {
				return err
			}
			if into[day] == nil {
				into[day] = map[string]int64{}
			}
			into[day][gw] = sum
		}
		return rows.Err()
	}
	if err := sums(d.Paid, `SELECT (paid_at + ?) / 86400, gateway, SUM(amount) FROM payments
		WHERE status IN ('paid', 'refunded') AND paid_at >= ? AND paid_at < ? GROUP BY 1, 2`, off, from, to); err != nil {
		return d, err
	}
	if err := sums(d.Refunded, `SELECT (COALESCE(refunded_at, paid_at) + ?) / 86400, gateway, SUM(amount) FROM payments
		WHERE status = 'refunded' AND COALESCE(refunded_at, paid_at) >= ? AND COALESCE(refunded_at, paid_at) < ? GROUP BY 1, 2`, off, from, to); err != nil {
		return d, err
	}
	one := func(dst *int, query string, args ...any) error {
		return s.db.R.QueryRowContext(ctx, query, args...).Scan(dst)
	}
	for _, c := range []struct {
		dst   *int
		query string
		args  []any
	}{
		{&d.NewUsers, "SELECT COUNT(*) FROM users WHERE created_at >= ? AND created_at < ?", []any{from, to}},
		{&d.Trials, `SELECT COUNT(*) FROM subscriptions s JOIN users u ON u.id = s.user_id
			WHERE u.trial_used = 1 AND s.created_at >= ? AND s.created_at < ?`, []any{from, to}},
		{&d.FirstPaid, `SELECT COUNT(*) FROM (SELECT MIN(paid_at) AS m FROM payments WHERE status IN ('paid', 'refunded') GROUP BY user_id)
			WHERE m >= ? AND m < ?`, []any{from, to}},
		{&d.ActiveSubs, "SELECT COUNT(*) FROM subscriptions WHERE status = 'active' AND created_at <= ? AND expires_at > ?", []any{q.At.Unix(), q.At.Unix()}},
		{&d.StuckTasks, `SELECT COUNT(*) FROM outbox WHERE done_at IS NULL AND created_at < ? AND kind NOT IN ('notify.admin', 'notify.owner')`, []any{q.StuckBefore.Unix()}},
	} {
		if err := one(c.dst, c.query, c.args...); err != nil {
			return d, err
		}
	}
	var err error
	d.StuckUsers, err = s.stuckUsers(ctx, q.StuckBefore)
	return d, err
}

// stuckUsers находит абонентов, у которых зависла задача: синхронизация с панелью по подписке или уведомление.
func (s *CoreStore) stuckUsers(ctx context.Context, before time.Time) ([]int64, error) {
	rows, err := s.db.R.QueryContext(ctx,
		`SELECT DISTINCT COALESCE(sub.user_id, json_extract(o.payload, '$.user_id'))
		 FROM outbox o LEFT JOIN subscriptions sub ON o.kind = 'panel.sync_user' AND sub.id = json_extract(o.payload, '$.sub_id')
		 WHERE o.done_at IS NULL AND o.created_at < ? AND o.kind IN ('panel.sync_user', 'notify.user', 'notify.confirm')
		   AND COALESCE(sub.user_id, json_extract(o.payload, '$.user_id')) IS NOT NULL
		 ORDER BY 1 LIMIT ?`, before.Unix(), maxStuckUsers)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}
