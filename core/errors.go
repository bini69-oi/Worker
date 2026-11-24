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
	ErrNotFound  = newErr("E1001", "err.not_found", "не найдено")
	ErrForbidden = newErr("E1002", "err.forbidden", "нет прав")
	ErrInvalid   = newErr("E1003", "err.invalid", "неверные данные")
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
	ErrNoPeriod     = newErr("E3001", "err.no_period", "у тарифа нет такого срока")
	ErrTariffHidden = newErr("E3002", "err.tariff_hidden", "тариф не продаётся")
	ErrOtherTariff  = newErr("E3003", "err.other_tariff", "у абонента живая подписка на другой тариф")
	ErrTrialOff     = newErr("E3004", "err.trial_off", "пробный период выключен")
	ErrTrialUsed    = newErr("E3005", "err.trial_used", "пробный период уже был")
	ErrBanned       = newErr("E3006", "err.banned", "абонент заблокирован")
	ErrPanelPending = newErr("E3007", "err.panel_pending", "подписка ещё оформляется")
	ErrPanelDown    = newErr("E3008", "err.panel_down", "сервис временно недоступен")
)

// ErrDuplicate значит, что событие с этой ссылкой уже было проведено.
// Это не ошибка для абонента: ядро трактует её как «уже сделано».
var ErrDuplicate = errors.New("событие уже проведено")
