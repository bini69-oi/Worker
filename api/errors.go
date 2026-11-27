package api

import (
	"errors"
	"net/http"

	"github.com/bini69-oi/worker/core"
)

// ErrorBody это тело любого ответа с ошибкой. Текст на языке абонента, код по нему находят запись в журнале.
type ErrorBody struct {
	Code string `json:"code"`
	Text string `json:"text"`
	// Detail это пояснение админу: какой тег не разрешён, какие подстановки допустимы. У абонента его нет.
	Detail string `json:"detail,omitempty"`
	// Money есть у ошибки «не хватает денег»: цена, баланс и недостача.
	Money *MoneyDetails `json:"money,omitempty"`
	// Limit есть у ошибки «сумма вне пределов кассы».
	Limit *LimitDetails `json:"limit,omitempty"`
}

// MoneyDetails это цифры покупки, на которую не хватило денег. Копейки.
type MoneyDetails struct {
	Price   int64 `json:"price"`
	Balance int64 `json:"balance"`
	Missing int64 `json:"missing"`
}

// LimitDetails это пределы суммы кассы в копейках, 0 значит без предела.
type LimitDetails struct {
	Min int64 `json:"min"`
	Max int64 `json:"max"`
}

// statuses это HTTP-статусы ошибок ядра. Ошибка ядра без записи здесь отвечает 409: запрос верный,
// но состояние абонента его не допускает (пробный уже был, смена тарифа уже ждёт).
var statuses = map[*core.Error]int{
	core.ErrNotFound:       http.StatusNotFound,
	core.ErrForbidden:      http.StatusForbidden,
	core.ErrBanned:         http.StatusForbidden,
	core.ErrOwnerOnly:      http.StatusForbidden,
	core.ErrInvalid:        http.StatusBadRequest,
	core.ErrTooLong:        http.StatusRequestEntityTooLarge,
	core.ErrBadMarkup:      http.StatusBadRequest,
	core.ErrBadText:        http.StatusBadRequest,
	core.ErrBadSetting:     http.StatusBadRequest,
	core.ErrPeriodTooLong:  http.StatusBadRequest,
	core.ErrZeroAmount:     http.StatusBadRequest,
	core.ErrAmountLimits:   http.StatusBadRequest,
	core.ErrNeedComment:    http.StatusBadRequest,
	core.ErrMaintenance:    http.StatusServiceUnavailable,
	core.ErrPanelDown:      http.StatusServiceUnavailable,
	core.ErrGatewayDown:    http.StatusBadGateway,
	core.ErrNotEnoughMoney: http.StatusPaymentRequired,
	core.ErrSessionGone:    http.StatusUnauthorized,
	core.ErrBadToken:       http.StatusUnauthorized,
	core.ErrBadLogin:       http.StatusUnauthorized,
	core.ErrNotConfirmed:   http.StatusUnauthorized,
	core.ErrTooManyTries:   http.StatusTooManyRequests,
	core.ErrLoginLocked:    http.StatusTooManyRequests,
	core.ErrInternal:       http.StatusInternalServerError,
	core.ErrBadInitData:    http.StatusUnauthorized,
	core.ErrLoginExpired:   http.StatusGone,
}

// classify превращает любую ошибку в ошибку ядра и статус. Всё, чего ядро не знает, это внутренняя ошибка.
func classify(err error) (*core.Error, int) {
	var ce *core.Error
	if !errors.As(err, &ce) {
		return core.ErrInternal, http.StatusInternalServerError
	}
	if st, ok := statuses[ce]; ok {
		return ce, st
	}
	return ce, http.StatusConflict
}

// fail отвечает ошибкой. Внутренняя ошибка уходит абоненту только кодом, подробности идут в журнал.
func (s *Server) fail(w http.ResponseWriter, r *http.Request, err error) {
	ce, status := classify(err)
	if ce == core.ErrInternal {
		s.deps.Log.Error("внутренняя ошибка API", "method", r.Method, "route", r.Pattern, "err", err)
	}
	lang := langOf(r.Context())
	var lerr *langError
	if errors.As(err, &lerr) {
		lang = lerr.lang
	}
	body := ErrorBody{Code: ce.Code, Text: s.deps.Text(ce.Key, lang)}
	var need *core.NeedMoney
	if errors.As(err, &need) {
		body.Money = &MoneyDetails{Price: need.Price, Balance: need.Balance, Missing: need.Missing}
	}
	var lim *core.LimitError
	if errors.As(err, &lim) {
		body.Limit = &LimitDetails{Min: lim.Min, Max: lim.Max}
	}
	var d interface{ Detail() string }
	if errors.As(err, &d) {
		body.Detail = d.Detail()
	}
	s.write(w, status, body)
}
