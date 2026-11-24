package core

import (
	"context"
	"io"
	"log/slog"
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
	// Tariffs отдаёт тарифы в порядке админа. onlyActive оставляет только те, что продаются.
	Tariffs(ctx context.Context, onlyActive bool) ([]Tariff, error)
	SubByUser(ctx context.Context, userID int64) (Subscription, error)
	// SubsInPanel отдаёт подписки, у которых уже есть абонент в панели. Нужно для сверки.
	SubsInPanel(ctx context.Context) ([]Subscription, error)

	// DueTasks отдаёт невыполненные задачи очереди, время которых пришло.
	DueTasks(ctx context.Context, now time.Time, limit int) ([]Task, error)
	TaskDone(ctx context.Context, id int64, at time.Time) error
	TaskFailed(ctx context.Context, id int64, attempts int, next time.Time, lastErr string) error
	// StuckTasks отдаёт невыполненные задачи старше before, о которых админу ещё не сообщали.
	// Уведомления админу сюда не попадают, иначе тревога о тревоге шла бы по кругу.
	StuckTasks(ctx context.Context, before time.Time) ([]Task, error)

	// PendingPayments отдаёт платежи «ожидает», созданные раньше before.
	PendingPayments(ctx context.Context, before time.Time) ([]Payment, error)
}

// Tx это действия внутри транзакции. Все изменения ядра идут только через Tx.
type Tx interface {
	User(id int64) (User, error)
	UserByTg(tgID int64) (User, error)
	CreateUser(u User) (int64, error)
	// AddLedger пишет строку истории и меняет кэш баланса. Повтор пары (RefType, RefID) даёт ErrDuplicate,
	// уход в минус даёт ErrNotEnoughMoney.
	AddLedger(e LedgerEntry) (int64, error)
	SetTrialUsed(userID int64) error

	Tariff(id int64) (Tariff, error)
	// SaveTariff создаёт тариф (ID == 0) или заменяет его вместе с периодами.
	SaveTariff(t Tariff) (int64, error)

	Sub(id int64) (Subscription, error)
	SubByUser(userID int64) (Subscription, error)
	// SaveSub создаёт подписку (ID == 0) или обновляет её.
	SaveSub(s Subscription) (int64, error)
	// ActiveSubsUntil отдаёт активные подписки, которые кончаются не позже t.
	ActiveSubsUntil(t time.Time) ([]Subscription, error)

	// Enqueue кладёт задачу в очередь (outbox). Она выполнится после коммита и будет повторяться до успеха.
	Enqueue(kind string, payload []byte, at time.Time) (int64, error)
	// AddNotice отмечает отправленное уведомление. false значит, что такое уже было.
	AddNotice(userID int64, kind, ref string, at time.Time) (bool, error)
	// MarkAlerted отмечает, что о задачах админу уже сообщили.
	MarkAlerted(ids []int64) error

	CreatePayment(p Payment) (int64, error)
	Payment(id int64) (Payment, error)
	PaymentByExt(gateway, externalID string) (Payment, error)
	// SetPaymentExt запоминает номер и сумму платежа в кассе.
	SetPaymentExt(id int64, externalID string, extAmount int64, extCurrency string) error
	// MarkPaid переводит платёж из «ожидает» в «оплачен». false значит, что платёж уже не ожидал.
	// Пустой externalID оставляет прежний номер.
	MarkPaid(id int64, externalID string, at time.Time) (bool, error)
	// SetPaymentStatus меняет статус, только если сейчас он from. false значит, что статус уже другой.
	SetPaymentStatus(id int64, from, to PayStatus) (bool, error)
}

// Notifier отправляет уведомления. Реализует его бот, до бота задачи ждут в очереди.
type Notifier interface {
	User(ctx context.Context, n NotifyTask) error
	Admin(ctx context.Context, n AdminTask) error
}

// Config это настройки, которые читает ядро. Реализует их пакет settings.
type Config interface {
	Bool(key string) bool
	Int(key string) int64
	String(key string) string
}

// Deps это всё, что ядру нужно снаружи.
type Deps struct {
	Store    Store
	Clock    Clock
	Config   Config
	Panel    Panel
	Notifier Notifier
	Gateways []Gateway
	Log      *slog.Logger
}

// Core это сценарии ядра. Бот, Mini App и админка зовут только их.
type Core struct {
	store    Store
	clock    Clock
	cfg      Config
	panel    Panel
	notifier Notifier
	gateways []Gateway
	log      *slog.Logger
	kick     chan struct{}
}

// New собирает ядро. Часы, уведомления и журнал можно не задавать: будут настоящие часы,
// очередь уведомлений будет ждать бота, журнал никуда не пишет.
func New(d Deps) *Core {
	if d.Clock == nil {
		d.Clock = SystemClock{}
	}
	if d.Notifier == nil {
		d.Notifier = noNotifier{}
	}
	if d.Log == nil {
		d.Log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return &Core{
		store: d.Store, clock: d.Clock, cfg: d.Config, panel: d.Panel,
		notifier: d.Notifier, gateways: d.Gateways, log: d.Log, kick: make(chan struct{}, 1),
	}
}

// Kick сигналит воркеру очереди, что появились задачи. Воркер слушает его вместе с таймером.
func (c *Core) Kick() <-chan struct{} { return c.kick }

// tx выполняет транзакцию и после успеха будит воркер очереди, чтобы задача ушла сразу.
func (c *Core) tx(ctx context.Context, fn func(Tx) error) error {
	if err := c.store.Tx(ctx, fn); err != nil {
		return err
	}
	select {
	case c.kick <- struct{}{}:
	default:
	}
	return nil
}
