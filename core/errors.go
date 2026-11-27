package core

import "errors"

// Error это ошибка, которую видит абонент или админ. У неё есть код вида E2001 и ключ текста.
// Абонент видит текст по ключу и код, админ по коду находит запись в журнале.
//
// Группы кодов: 1xxx общие, 2xxx деньги, 3xxx подписки, 4xxx кассы,
// 5xxx промокоды и рефералка, 6xxx безопасность.
type Error struct {
	Code string
	Key  string
	msg  string
}

func (e *Error) Error() string { return e.Code + ": " + e.msg }

var allErrors []*Error

func newErr(code, key, msg string) *Error {
	e := &Error{Code: code, Key: key, msg: msg}
	allErrors = append(allErrors, e)
	return e
}

// Общие
var (
	ErrNotFound    = newErr("E1001", "err.not_found", "не найдено")
	ErrForbidden   = newErr("E1002", "err.forbidden", "нет прав")
	ErrInvalid     = newErr("E1003", "err.invalid", "неверные данные")
	ErrTooLong     = newErr("E1004", "err.too_long", "слишком длинный текст")
	ErrMaintenance = newErr("E1005", "err.maintenance", "идут технические работы, загляните позже")
	ErrInternal    = newErr("E1006", "err.internal", "внутренняя ошибка")
)

// Деньги
var (
	ErrNotEnoughMoney = newErr("E2001", "err.not_enough_money", "не хватает денег")
	ErrZeroAmount     = newErr("E2002", "err.zero_amount", "сумма должна быть не нулевой")
	ErrNeedComment    = newErr("E2003", "err.need_comment", "нужен комментарий")
)

// NeedMoney это «не хватает денег» с суммой, которой не хватает. errors.Is(err, ErrNotEnoughMoney) для неё истинно.
type NeedMoney struct {
	Missing int64 // копейки
}

func (e *NeedMoney) Error() string { return ErrNotEnoughMoney.Error() }
func (e *NeedMoney) Unwrap() error { return ErrNotEnoughMoney }

// Подписки
var (
	ErrNoPeriod      = newErr("E3001", "err.no_period", "у тарифа нет такого срока")
	ErrTariffHidden  = newErr("E3002", "err.tariff_hidden", "тариф не продаётся")
	ErrTrialOff      = newErr("E3004", "err.trial_off", "пробный период выключен")
	ErrTrialUsed     = newErr("E3005", "err.trial_used", "пробный период уже был")
	ErrTrialBusy     = newErr("E3009", "err.trial_busy", "пробные периоды на этот час закончились, попробуйте позже")
	ErrBanned        = newErr("E3006", "err.banned", "абонент заблокирован")
	ErrPanelPending  = newErr("E3007", "err.panel_pending", "подписка ещё оформляется")
	ErrPanelDown     = newErr("E3008", "err.panel_down", "сервис временно недоступен")
	ErrNoSub         = newErr("E3010", "err.no_sub", "у абонента нет подписки")
	ErrNotSubscribed = newErr("E3011", "err.not_subscribed", "сначала подпишитесь на канал")
	ErrPlanQueued    = newErr("E3012", "err.plan_queued", "уже ждёт смена тарифа: отмените её или дождитесь начала")
	ErrUnlimited     = newErr("E3013", "err.unlimited", "в тарифе и так без лимита")
	ErrAddonOff      = newErr("E3014", "err.addon_off", "докупка выключена")
	ErrOtherTariff   = newErr("E3003", "err.other_tariff", "у абонента подписка на другой тариф")
)

// Кассы
var (
	ErrAmountLimits   = newErr("E4001", "err.amount_limits", "сумма вне пределов кассы")
	ErrGatewayDown    = newErr("E4002", "err.gateway_down", "касса не ответила")
	ErrAmountMismatch = newErr("E4003", "err.amount_mismatch", "оплачена другая сумма")
	ErrNoGateway      = newErr("E4004", "err.no_gateway", "касса недоступна")
	ErrRefundFailed   = newErr("E4005", "err.refund_failed", "касса отказала в возврате")
)

// Промокоды и рефералка
var (
	ErrPromoNotFound  = newErr("E5001", "err.promo_not_found", "такого кода нет")
	ErrPromoUsed      = newErr("E5002", "err.promo_used", "этот код уже активирован")
	ErrPromoExpired   = newErr("E5003", "err.promo_expired", "код истёк")
	ErrPromoExhausted = newErr("E5004", "err.promo_exhausted", "код больше не действует")
	ErrPromoNewOnly   = newErr("E5005", "err.promo_new_only", "код только для новых абонентов")
	ErrPromoNeedSub   = newErr("E5006", "err.promo_need_sub", "код продлевает подписку, а её нет")
	ErrTooManyTries   = newErr("E5007", "err.too_many_tries", "слишком много попыток, попробуйте позже")
)

// Безопасность
var (
	ErrBadLogin     = newErr("E6001", "err.bad_login", "неверный логин или пароль")
	ErrLoginLocked  = newErr("E6002", "err.login_locked", "слишком много попыток входа, подождите")
	ErrConfirmGone  = newErr("E6003", "err.confirm_gone", "подтверждение устарело или уже использовано")
	ErrNotConfirmed = newErr("E6004", "err.not_confirmed", "вход не подтверждён в Telegram")
	ErrWeakPassword = newErr("E6005", "err.weak_password", "пароль короче 10 символов")
	ErrNoPassword   = newErr("E6006", "err.no_password", "пароль владельца не задан")
	ErrBadToken     = newErr("E6007", "err.bad_token", "Telegram не принял токен")
	ErrSessionGone  = newErr("E6008", "err.session_gone", "сессия закрыта или истекла")
	ErrOwnerOnly    = newErr("E6009", "err.owner_only", "это может только владелец")
)

// ErrDuplicate значит, что событие с этой ссылкой уже было проведено.
// Это не ошибка для абонента: ядро трактует её как «уже сделано».
var ErrDuplicate = errors.New("событие уже проведено")

// Errors отдаёт все ошибки ядра. Тесты проверяют по нему, что у каждой есть текст.
func Errors() []*Error { return append([]*Error(nil), allErrors...) }
