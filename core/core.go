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
	RefStats(ctx context.Context, userID int64) (RefStats, error)
	Admins(ctx context.Context) ([]Admin, error)
	// Sessions отдаёт сессии админки, которые открыты и не истекли к моменту now.
	Sessions(ctx context.Context, now time.Time) ([]Session, error)
	// SubsInPanel отдаёт подписки, у которых уже есть абонент в панели. Нужно для сверки.
	SubsInPanel(ctx context.Context) ([]Subscription, error)

	// DueTasks отдаёт невыполненные задачи очереди, время которых пришло.
	DueTasks(ctx context.Context, now time.Time, limit int) ([]Task, error)
	TaskDone(ctx context.Context, id int64, at time.Time) error
	TaskFailed(ctx context.Context, id int64, attempts int, next time.Time, lastErr string) error
	// StuckTasks отдаёт невыполненные задачи старше before, о которых админу ещё не сообщали.
	// Уведомления админу сюда не попадают, иначе тревога о тревоге шла бы по кругу.
	StuckTasks(ctx context.Context, before time.Time) ([]Task, error)

	// DeleteSupportBefore удаляет связи поддержки старше before.
	DeleteSupportBefore(ctx context.Context, before time.Time) (int, error)

	// PendingPayments отдаёт платежи «ожидает», созданные раньше before.
	PendingPayments(ctx context.Context, before time.Time) ([]Payment, error)
}

// Tx это действия внутри транзакции. Все изменения ядра идут только через Tx.
type Tx interface {
	User(id int64) (User, error)
	UserByTg(tgID int64) (User, error)
	// CreateUser создаёт абонента. Повтор Telegram ID или реферального кода даёт ErrDuplicate.
	CreateUser(u User) (int64, error)
	UserByRef(code string) (User, error)
	// AddLedger пишет строку истории и меняет кэш баланса. Повтор пары (RefType, RefID) даёт ErrDuplicate,
	// уход в минус даёт ErrNotEnoughMoney.
	AddLedger(e LedgerEntry) (int64, error)
	SetTrialUsed(userID int64) error
	// SetUnreachable отмечает, что абонент заблокировал бота. Нулевое время снимает отметку.
	SetUnreachable(userID int64, at time.Time) error
	// SetBanned ставит бан. Нулевое время снимает его.
	SetBanned(userID int64, at time.Time) error
	// AddSupportMsg запоминает, что сообщение в группе поддержки относится к абоненту.
	AddSupportMsg(chatMsgID, userID int64, at time.Time) error
	// SupportMsg отдаёт абонента и время связи по сообщению в группе. Нет связи: ErrNotFound.
	SupportMsg(chatMsgID int64) (userID int64, at time.Time, err error)
	// SetLang меняет язык абонента.
	SetLang(userID int64, lang string) error
	// SetScreenMsg запоминает сообщение-экран абонента.
	SetScreenMsg(userID, msgID int64) error

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
	// Paid говорит, есть ли у абонента хоть один оплаченный платёж: платил ли он настоящими деньгами.
	Paid(userID int64) (bool, error)
	// SetPaymentExt запоминает номер и сумму платежа в кассе.
	SetPaymentExt(id int64, externalID string, extAmount int64, extCurrency string) error
	// MarkPaid переводит платёж в «оплачен» из «ожидает», «истёк» или «ошибка»: деньги могли прийти позже срока.
	// false значит, что платёж уже оплачен или возвращён.
	// Пустой externalID оставляет прежний номер.
	MarkPaid(id int64, externalID string, at time.Time) (bool, error)
	// Promo ищет промокод без учёта регистра.
	Promo(code string) (Promo, error)
	SavePromo(p Promo) (int64, error)
	// UsePromo отмечает активацию: повтор того же абонента даёт ErrDuplicate, исчерпанный лимит ErrPromoExhausted.
	UsePromo(promoID, userID int64, at time.Time) error

	// Limit читает счётчик попыток по ключу (например promo:42). Нет счётчика: 0 и нулевое время.
	Limit(key string) (count int, until time.Time, err error)
	SetLimit(key string, count int, until time.Time) error

	Admin(tgID int64) (Admin, error)
	// SaveAdmin создаёт или заменяет админа.
	SaveAdmin(a Admin) error
	// AddAudit пишет строку журнала действий. Правки и удаления журнала нет.
	AddAudit(a AuditEntry) error

	CreateConfirm(cf Confirm) (int64, error)
	Confirm(id int64) (Confirm, error)
	// AnswerConfirm записывает ответ, только если ответа ещё нет и срок не вышел. false значит поздно.
	AnswerConfirm(id int64, answer string, at time.Time) (bool, error)
	// UseConfirm отмечает, что подтверждением воспользовались. Второй раз false.
	UseConfirm(id int64, at time.Time) (bool, error)

	CreateSession(tokenHash []byte, s Session) error
	Session(tokenHash []byte) (Session, error)
	// TouchSession запоминает время последнего запроса сессии.
	TouchSession(tokenHash []byte, at time.Time) error
	CloseSession(id string, at time.Time) error
	CloseAdminSessions(adminTg int64, at time.Time) error

	AdminChat() (AdminChat, error)
	SaveAdminChat(ch AdminChat) error

	SetPendingBuy(pb PendingBuy) error
	// PendingBuy отдаёт запомненный выбор абонента. Нет выбора: ErrNotFound.
	PendingBuy(userID int64) (PendingBuy, error)
	DeletePendingBuy(userID int64) error

	// SetPaymentStatus меняет статус, только если сейчас он from. false значит, что статус уже другой.
	SetPaymentStatus(id int64, from, to PayStatus) (bool, error)
}

// Notifier отправляет уведомления. Реализует его бот, до бота задачи ждут в очереди.
type Notifier interface {
	User(ctx context.Context, n NotifyTask) error
	Admin(ctx context.Context, n AdminTask) error
	// Confirm шлёт админу в личку сообщение с кнопками «Да» и «Нет».
	Confirm(ctx context.Context, n ConfirmTask) error
}

// Channels говорит, подписан ли человек на канал. Реализует бот через getChatMember.
type Channels interface {
	IsMember(ctx context.Context, channel string, tgID int64) (bool, error)
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
	Telegram TelegramAPI
	// Channels проверяет подписку на канал перед пробным периодом. Может быть nil: проверки не будет.
	Channels Channels
	// OwnerID это Telegram ID владельца из окружения. Изнутри системы он не меняется.
	OwnerID int64
	// Secrets это фильтр секретов журнала. Ядро добавляет в него новые ключи после их смены
	// и пропускает через него тексты ошибок, которые пишет в базу. Может быть nil.
	Secrets SecretFilter
	Log     *slog.Logger
}

// SecretFilter это фильтр секретов: запомнить секрет и вычистить известные секреты из строки.
type SecretFilter interface {
	Add(vals ...string)
	String(text string) string
}

// Core это сценарии ядра. Бот, Mini App и админка зовут только их.
type Core struct {
	store    Store
	clock    Clock
	cfg      Config
	panel    Panel
	notifier Notifier
	gateways []Gateway
	telegram TelegramAPI
	channels Channels
	owner    int64
	log      *slog.Logger
	kick     chan struct{}
	secrets  pending
	filter   SecretFilter
	actions  map[string]action
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
		notifier: d.Notifier, gateways: d.Gateways, telegram: d.Telegram, channels: d.Channels, owner: d.OwnerID, log: d.Log,
		kick: make(chan struct{}, 1), actions: map[string]action{}, filter: d.Secrets,
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
