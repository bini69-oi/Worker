package core

import (
	"context"
	"time"
)

// Clock даёт текущее время. Тесты подставляют часы, которые двигают руками.
type Clock interface {
	Now() time.Time
}

// SystemClock это настоящие часы.
type SystemClock struct{}

func (SystemClock) Now() time.Time { return time.Now() }

// Store это хранилище ядра. Реализует его пакет store на SQLite, в тестах стоит фейк из coretest.
type Store interface {
	// Tx выполняет fn в одной транзакции: либо всё, либо ничего.
	Tx(ctx context.Context, fn func(Tx) error) error
	User(ctx context.Context, id int64) (User, error)
	UserByTg(ctx context.Context, tgID int64) (User, error)
	// Ledger отдаёт историю денег абонента, новые строки первыми.
	Ledger(ctx context.Context, userID int64, limit int) ([]LedgerEntry, error)
}

// Tx это действия внутри транзакции. Все изменения ядра идут только через Tx.
type Tx interface {
	User(id int64) (User, error)
	UserByTg(tgID int64) (User, error)
	CreateUser(u User) (int64, error)
	// AddLedger пишет строку истории и меняет кэш баланса. Повтор пары (RefType, RefID) даёт ErrDuplicate,
	// уход в минус даёт ErrNotEnoughMoney.
	AddLedger(e LedgerEntry) (int64, error)
}

// Deps это всё, что ядру нужно снаружи.
type Deps struct {
	Store Store
	Clock Clock
}

// Core это сценарии ядра. Бот, Mini App и админка зовут только их.
type Core struct {
	store Store
	clock Clock
}

// New собирает ядро. Если часы не заданы, берутся настоящие.
func New(d Deps) *Core {
	if d.Clock == nil {
		d.Clock = SystemClock{}
	}
	return &Core{store: d.Store, clock: d.Clock}
}
