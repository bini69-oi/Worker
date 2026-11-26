package core

import (
	"encoding/json"
	"time"
)

// Виды задач в очереди (outbox).
const (
	// KindPanelSync приводит абонента в панели к состоянию подписки из базы.
	KindPanelSync = "panel.sync_user"
	// KindNotifyUser шлёт абоненту текст по ключу.
	KindNotifyUser = "notify.user"
	// KindNotifyAdmin шлёт событие в тему админ-чата.
	KindNotifyAdmin = "notify.admin"
)

// Темы админ-чата.
const (
	TopicPayments = "payments"
	TopicNew      = "new"
	TopicErrors   = "errors"
	TopicSecurity = "security"
)

// AdminTask это событие для админов: тема, ключ текста и подстановки.
type AdminTask struct {
	Topic string            `json:"topic"`
	Key   string            `json:"key"`
	Vars  map[string]string `json:"vars,omitempty"`
}

// PanelSyncTask это задача синхронизации: в ней только id подписки, состояние берётся из базы в момент выполнения.
type PanelSyncTask struct {
	SubID int64 `json:"sub_id"`
}

// NotifyTask это уведомление абоненту: ключ текста и подстановки.
type NotifyTask struct {
	UserID int64             `json:"user_id"`
	Key    string            `json:"key"`
	Vars   map[string]string `json:"vars,omitempty"`
}

func enqueueSync(tx Tx, subID int64, now time.Time) error {
	body, err := json.Marshal(PanelSyncTask{SubID: subID})
	if err != nil {
		return err
	}
	_, err = tx.Enqueue(KindPanelSync, body, now)
	return err
}

func enqueueAdmin(tx Tx, n AdminTask, now time.Time) error {
	body, err := json.Marshal(n)
	if err != nil {
		return err
	}
	_, err = tx.Enqueue(KindNotifyAdmin, body, now)
	return err
}

// enqueueNotify ставит уведомление абоненту. Абоненту, который заблокировал бота, не ставит:
// Telegram всё равно откажет, а очередь будет повторять задачу впустую.
func enqueueNotify(tx Tx, n NotifyTask, now time.Time) error {
	u, err := tx.User(n.UserID)
	if err != nil {
		return err
	}
	if !u.UnreachableAt.IsZero() {
		return nil
	}
	body, err := json.Marshal(n)
	if err != nil {
		return err
	}
	_, err = tx.Enqueue(KindNotifyUser, body, now)
	return err
}
