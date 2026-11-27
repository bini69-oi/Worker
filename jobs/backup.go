package jobs

import (
	"context"
	"time"
)

// Backuper это то, что умеет делать бэкап, когда он нужен (раз в сутки), и чистить старые.
type Backuper interface {
	RunDue(ctx context.Context) error
}

// BackupJob это ежедневный бэкап. Задача просыпается раз в час и сама решает, прошли ли сутки с последнего:
// так перезапуск службы не сдвигает расписание и не плодит бэкапы.
func BackupJob(b Backuper) Job {
	return Job{Name: "backup", Every: fixed(time.Hour), Run: b.RunDue}
}
