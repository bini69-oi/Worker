package core

import "time"

// User это абонент бота.
type User struct {
	ID         int64
	TgID       int64
	Username   string
	Lang       string
	ReferrerID int64 // 0, если абонент пришёл сам
	Balance    int64 // копейки, кэш суммы истории денег
	TrialUsed  bool
	BannedAt   time.Time // нулевое время, если бана нет
	CreatedAt  time.Time
}

// Tariff это тариф: лимиты и группы серверов в панели.
type Tariff struct {
	ID           int64
	Name         string
	TrafficBytes int64    // 0 значит без лимита
	Devices      int      // 0 значит без лимита
	Reset        string   // когда панель сбрасывает трафик, например NO_RESET или MONTH
	Squads       []string // группы серверов в Remnawave
	Active       bool
	Sort         int
	Periods      []Period
	CreatedAt    time.Time
}

// Period это срок тарифа и его цена.
type Period struct {
	Days  int
	Price int64 // копейки
}

// SubStatus это состояние подписки.
type SubStatus string

const (
	SubActive   SubStatus = "active"
	SubExpired  SubStatus = "expired"
	SubDisabled SubStatus = "disabled"
)

// Subscription это подписка абонента. Цена и срок заморожены на момент покупки.
type Subscription struct {
	ID         int64
	UserID     int64
	TariffID   int64
	PeriodDays int
	Price      int64
	Trial      bool
	PanelUUID  string
	SubURL     string
	ExpiresAt  time.Time
	AutoRenew  bool
	Status     SubStatus
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// Reason это причина движения денег.
type Reason string

const (
	ReasonTopup    Reason = "topup"
	ReasonBuy      Reason = "buy"
	ReasonPromo    Reason = "promo"
	ReasonReferral Reason = "referral"
	ReasonAdmin    Reason = "admin"
	ReasonRefund   Reason = "refund"
)

// LedgerEntry это одна строка истории денег. Сумма со знаком: плюс зачисление, минус списание.
type LedgerEntry struct {
	ID        int64
	UserID    int64
	Amount    int64
	Reason    Reason
	RefType   string // откуда движение: payment, refund, promo и т.п.
	RefID     int64  // 0, если ссылки нет; пара (RefType, RefID) двигает деньги один раз
	AdminID   int64  // 0, если правка не от админа
	Comment   string
	CreatedAt time.Time
}

// PayStatus это состояние платежа.
type PayStatus string

const (
	PayPending  PayStatus = "pending"
	PayPaid     PayStatus = "paid"
	PayExpired  PayStatus = "expired"
	PayRefunded PayStatus = "refunded"
	PayFailed   PayStatus = "failed"
)

// Payment это платёж через кассу.
type Payment struct {
	ID          int64
	UserID      int64
	Gateway     string
	Amount      int64 // копейки
	ExtAmount   int64 // сумма в валюте кассы, например в звёздах
	ExtCurrency string
	Recurring   bool
	Status      PayStatus
	ExternalID  string
	CreatedAt   time.Time
	PaidAt      time.Time
}
