package core

import (
	"context"
	"time"
)

// PageSize это размер страницы списков админки. Страницы идут по курсору (id последней строки), а не по номеру:
// новые строки не сдвигают уже открытые страницы.
const PageSize = 50

// Page это страница списка. Next это курсор следующей страницы, 0 значит, что дальше ничего нет.
type Page[T any] struct {
	Items []T
	Next  int64
}

// Фильтры абонентов и сегменты рассылки говорят об одном и том же: «с подпиской» это живая подписка
// (пробная тоже), «пробный» живая пробная, «истекла» подписка была и кончилась, «без подписки» её не было.
const (
	FilterAll     = "all"
	FilterActive  = "active"
	FilterTrial   = "trial"
	FilterExpired = "expired"
	FilterNone    = "none"
	FilterBanned  = "banned"
	// SegExpired30 это подписка, которая кончилась не больше 30 дней назад. Только для рассылок.
	SegExpired30 = "expired30"
)

// UserFilters это допустимые фильтры поиска абонентов.
var UserFilters = []string{FilterAll, FilterActive, FilterTrial, FilterExpired, FilterNone, FilterBanned}

// UserQuery это разобранный запрос поиска. Ядро разбирает текст админа, хранилище только исполняет.
type UserQuery struct {
	TgID           int64  // точный Telegram ID, 0 если не ищем
	RefCode        string // точный код рефералки
	UsernamePrefix string // начало @username без @, без учёта регистра
	Filter         string // один из UserFilters, пусто как all
	After          int64  // курсор: только абоненты с id меньше
}

// UserRow это строка поиска: абонент и коротко его подписка.
type UserRow struct {
	// Status ставит ядро: none, trial, active или expired.
	Status    string
	User      User
	HasSub    bool
	SubStatus SubStatus
	Trial     bool
	ExpiresAt time.Time
}

// Class говорит, что у абонента с подпиской на момент now: none, trial, active (платная) или expired.
func (r UserRow) Class(now time.Time) string {
	switch {
	case !r.HasSub:
		return FilterNone
	case r.SubStatus != SubActive || !r.ExpiresAt.After(now):
		return FilterExpired
	case r.Trial:
		return FilterTrial
	}
	return FilterActive
}

// Matches говорит, подходит ли строка фильтру. Фейковое хранилище в тестах судит по тому же правилу,
// что SQL настоящего.
func (r UserRow) Matches(filter string, now time.Time) bool {
	switch filter {
	case "", FilterAll:
		return true
	case FilterBanned:
		return !r.User.BannedAt.IsZero()
	case FilterActive:
		c := r.Class(now)
		return c == FilterActive || c == FilterTrial
	case SegExpired30:
		return r.Class(now) == FilterExpired && r.ExpiresAt.After(now.Add(-30*24*time.Hour))
	}
	return r.Class(now) == filter
}

// AuditRow это строка журнала с номером для курсора.
type AuditRow struct {
	ID int64
	AuditEntry
}

// AuditFilter это фильтр журнала действий. Нулевые поля не фильтруют.
type AuditFilter struct {
	AdminTg int64
	Action  string
	Target  string
	From    time.Time // включительно
	To      time.Time // не включая
	After   int64     // курсор: только записи с id меньше
}

// PaymentFilter это фильтр списка платежей. Нулевые поля не фильтруют.
type PaymentFilter struct {
	Status  PayStatus
	Gateway string
	From    time.Time // создан не раньше
	To      time.Time // создан раньше
	After   int64     // курсор: только платежи с id меньше
}

// PaymentRow это платёж с абонентом для списка.
type PaymentRow struct {
	Payment  Payment
	Username string
	TgID     int64
}

// AdminStore это чтение для админки: списки, поиск, журнал. Пишет админка только через Tx, как всё ядро.
type AdminStore interface {
	// FindUsers ищет абонентов, новые первыми. Отдаёт не больше limit строк.
	FindUsers(ctx context.Context, q UserQuery, now time.Time, limit int) ([]UserRow, error)
	// Broadcasts отдаёт рассылки, новые первыми.
	Broadcasts(ctx context.Context, after int64, limit int) ([]Broadcast, error)
	BroadcastByID(ctx context.Context, id int64) (Broadcast, error)
	// SegmentCount считает получателей сегмента: без забаненных и недоступных.
	SegmentCount(ctx context.Context, segment string, now time.Time) (int, error)
	// SegmentUsers отдаёт получателей сегмента с id больше after по возрастанию.
	SegmentUsers(ctx context.Context, segment string, after int64, now time.Time, limit int) ([]User, error)
	// PromoList отдаёт промокоды с числом активаций, новые первыми.
	PromoList(ctx context.Context, after int64, limit int) ([]Promo, error)
	// PromoByID отдаёт промокод. Нет такого: ErrNotFound.
	PromoByID(ctx context.Context, id int64) (Promo, error)
	// Payments отдаёт платежи всех касс с абонентами, новые первыми.
	Payments(ctx context.Context, f PaymentFilter, limit int) ([]PaymentRow, error)
	// UserPayments отдаёт платежи абонента, новые первыми.
	UserPayments(ctx context.Context, userID int64, limit int) ([]Payment, error)
	// AuditLog отдаёт записи журнала, новые первыми.
	AuditLog(ctx context.Context, f AuditFilter, limit int) ([]AuditRow, error)
}

// AdminTx это записи админки внутри транзакции: рассылки.
type AdminTx interface {
	CreateBroadcast(b Broadcast) (int64, error)
	// Broadcast отдаёт рассылку. Нет такой: ErrNotFound.
	Broadcast(id int64) (Broadcast, error)
	// RunningBroadcast отдаёт идущую рассылку. Нет идущей: ErrNotFound.
	RunningBroadcast() (Broadcast, error)
	// StartBroadcast переводит черновик в «идёт». false значит, что это уже не черновик. Вторая идущая рассылка
	// даёт ErrDuplicate: база не пускает две сразу.
	StartBroadcast(id int64, total int, at time.Time) (bool, error)
	// CancelBroadcast отменяет черновик или идущую рассылку. false значит, что она уже закончена.
	CancelBroadcast(id int64, at time.Time) (bool, error)
	// BroadcastStep записывает итог по одному абоненту и двигает курсор, только если рассылка идёт.
	// false значит, что её уже отменили: пачку надо остановить.
	BroadcastStep(id, userID int64, sent, failed, blocked int) (bool, error)
	// FinishBroadcast переводит идущую рассылку в «готово».
	FinishBroadcast(id int64, at time.Time) (bool, error)
	// SegmentCount считает получателей сегмента: без забаненных и недоступных.
	SegmentCount(segment string, now time.Time) (int, error)
}
