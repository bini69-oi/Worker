package api

import (
	"reflect"
	"time"
)

// Запросы и ответы кабинета. Структуры ответов попадают в types.gen.ts через appTypes.

// EmptyRequest это тело запроса без полей: `{}`.
type EmptyRequest struct{}

// SessionTgRequest это вход из Mini App.
type SessionTgRequest struct {
	InitData string `json:"init_data"`
}

// SessionResponse это сессия Mini App: токен идёт заголовком Authorization.
type SessionResponse struct {
	Token     string    `json:"token"`
	Lang      string    `json:"lang"`
	ExpiresAt time.Time `json:"expires_at"`
}

func appTypes() []reflect.Type {
	return []reflect.Type{
		reflect.TypeFor[SessionResponse](),
		reflect.TypeFor[LoginStartResponse](),
		reflect.TypeFor[LoginPollResponse](),
		reflect.TypeFor[MeResponse](),
		reflect.TypeFor[TextsResponse](),
		reflect.TypeFor[SubView](),
		reflect.TypeFor[TariffsResponse](),
		reflect.TypeFor[QuoteResponse](),
		reflect.TypeFor[BuyResponse](),
		reflect.TypeFor[TopupResponse](),
		reflect.TypeFor[PaymentResponse](),
	}
}

// MeResponse это главная кабинета.
type MeResponse struct {
	Lang           string        `json:"lang"`
	Username       string        `json:"username"`
	Balance        int64         `json:"balance"` // копейки
	Maintenance    bool          `json:"maintenance"`
	TrialAvailable bool          `json:"trial_available"`
	Sub            *SubView      `json:"sub"`
	Gateways       []GatewayView `json:"gateways"`
	BotLink        string        `json:"bot_link"` // чат бота: поддержка и вход
}

// GatewayView это касса для абонента.
type GatewayView struct {
	Name  string `json:"name"`
	Title string `json:"title"`
}

// SubView это подписка абонента. State: active, trial или expired. Лимиты 0 значат «без лимита».
type SubView struct {
	State             string        `json:"state"`
	Trial             bool          `json:"trial"`
	Imported          bool          `json:"imported"`
	TariffID          int64         `json:"tariff_id"`
	Tariff            string        `json:"tariff"` // пусто у пробной и перенесённой подписки
	ExpiresAt         time.Time     `json:"expires_at"`
	DaysLeft          int           `json:"days_left"`
	AutoRenew         bool          `json:"auto_renew"`
	TrafficBytes      int64         `json:"traffic_bytes"`
	Devices           int           `json:"devices"`
	ExtraTrafficBytes int64         `json:"extra_traffic_bytes"`
	ExtraDevices      int           `json:"extra_devices"`
	Next              *NextPlanView `json:"next"`
}

// NextPlanView это оплаченная смена тарифа, которая включится, когда кончится текущий период.
type NextPlanView struct {
	TariffID int64     `json:"tariff_id"`
	Tariff   string    `json:"tariff"`
	Days     int       `json:"days"`
	StartsAt time.Time `json:"starts_at"`
}

// TextsResponse это тексты кабинета на языке абонента.
type TextsResponse struct {
	Lang  string            `json:"lang"`
	Texts map[string]string `json:"texts"`
}

// BuyRequest это выбор тарифа и срока для расчёта и покупки.
type BuyRequest struct {
	TariffID int64 `json:"tariff_id"`
	Days     int   `json:"days"`
}

// TariffsResponse это тарифы, которые продаются.
type TariffsResponse struct {
	Tariffs []TariffView `json:"tariffs"`
}

// TariffView это тариф со сроками. Лимиты 0 значат «без лимита».
type TariffView struct {
	ID           int64        `json:"id"`
	Name         string       `json:"name"`
	TrafficBytes int64        `json:"traffic_bytes"`
	Devices      int          `json:"devices"`
	Periods      []PeriodView `json:"periods"`
}

// PeriodView это срок тарифа и цена в копейках.
type PeriodView struct {
	Days  int   `json:"days"`
	Price int64 `json:"price"`
}

// QuoteResponse это расчёт покупки. StartsAt есть, когда тариф другой и включится с конца текущего периода.
type QuoteResponse struct {
	Price    int64      `json:"price"`
	Balance  int64      `json:"balance"`
	Missing  int64      `json:"missing"`
	Queued   bool       `json:"queued"`
	StartsAt *time.Time `json:"starts_at"`
}

// BuyResponse это подписка после покупки. Queued: покупка другого тарифа ждёт конца текущего периода.
type BuyResponse struct {
	Sub    SubView `json:"sub"`
	Queued bool    `json:"queued"`
}

// TopupRequest это пополнение: касса и сумма в копейках.
type TopupRequest struct {
	Gateway string `json:"gateway"`
	Amount  int64  `json:"amount"`
}

// TopupResponse это созданный платёж. URL есть у касс со ссылкой, InvoiceLink у Stars, Details у ручной кассы.
type TopupResponse struct {
	PaymentID   int64  `json:"payment_id"`
	Gateway     string `json:"gateway"`
	URL         string `json:"url"`
	InvoiceLink string `json:"invoice_link"`
	Details     string `json:"details"`
	ExtAmount   int64  `json:"ext_amount"`
	ExtCurrency string `json:"ext_currency"`
}

// PaymentResponse это платёж абонента. Status: pending, paid, expired, refunded или failed.
type PaymentResponse struct {
	ID        int64      `json:"id"`
	Gateway   string     `json:"gateway"`
	Amount    int64      `json:"amount"`
	Status    string     `json:"status"`
	CreatedAt time.Time  `json:"created_at"`
	PaidAt    *time.Time `json:"paid_at"`
}
