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
)

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

func enqueueNotify(tx Tx, n NotifyTask, now time.Time) error {
	body, err := json.Marshal(n)
	if err != nil {
		return err
	}
	_, err = tx.Enqueue(KindNotifyUser, body, now)
	return err
}
