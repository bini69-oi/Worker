package core

// Ошибки админки. Номера взяты с отступом от общих рядов (E1051 вместо следующего свободного),
// чтобы ошибки других частей рядом не пересекались с ними.
var (
	ErrBadMarkup     = newErr("E1051", "err.bad_markup", "разметка не прошла проверку")
	ErrBadText       = newErr("E1052", "err.bad_text", "текст не подходит")
	ErrBadSetting    = newErr("E1053", "err.bad_setting", "значение настройки не подходит")
	ErrPeriodTooLong = newErr("E1054", "err.period_too_long", "период больше 90 дней")
	ErrBroadcastBusy = newErr("E1055", "err.broadcast_busy", "дождитесь конца текущей рассылки")
	ErrUnreachable   = newErr("E1056", "err.unreachable", "абонент заблокировал бота")
	ErrAlreadyPaid   = newErr("E2050", "err.already_paid", "платёж уже зачислен")
	ErrNoKeys        = newErr("E4050", "err.no_keys", "сначала заполните ключи кассы")
)

// detailed это ошибка ядра с пояснением для админа: какой тег не разрешён, какие подстановки допустимы.
type detailed struct {
	base   *Error
	detail string
}

func (e *detailed) Error() string  { return e.base.Error() + ": " + e.detail }
func (e *detailed) Unwrap() error  { return e.base }
func (e *detailed) Detail() string { return e.detail }

// WithDetail добавляет к ошибке пояснение. errors.Is и errors.As видят в ней исходную ошибку.
func WithDetail(base *Error, detail string) error { return &detailed{base: base, detail: detail} }
