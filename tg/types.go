// Пакет tg это клиент Telegram Bot API на стандартной библиотеке.
package tg

import "encoding/json"

// Update это одно обновление от Telegram.
type Update struct {
	UpdateID         int64              `json:"update_id"`
	Message          *Message           `json:"message,omitempty"`
	CallbackQuery    *CallbackQuery     `json:"callback_query,omitempty"`
	PreCheckoutQuery *PreCheckoutQuery  `json:"pre_checkout_query,omitempty"`
	MyChatMember     *ChatMemberUpdated `json:"my_chat_member,omitempty"`
}

// User это пользователь или бот.
type User struct {
	ID           int64  `json:"id"`
	IsBot        bool   `json:"is_bot,omitempty"`
	FirstName    string `json:"first_name,omitempty"`
	LastName     string `json:"last_name,omitempty"`
	Username     string `json:"username,omitempty"`
	LanguageCode string `json:"language_code,omitempty"`
}

// Chat это чат: личка, группа или канал.
type Chat struct {
	ID       int64  `json:"id"`
	Type     string `json:"type,omitempty"`
	Title    string `json:"title,omitempty"`
	Username string `json:"username,omitempty"`
	IsForum  bool   `json:"is_forum,omitempty"`
}

// Message это сообщение. Фото и документ не разбираем: нужно знать только, что они есть.
type Message struct {
	MessageID         int                `json:"message_id"`
	ThreadID          int                `json:"message_thread_id,omitempty"`
	IsTopicMessage    bool               `json:"is_topic_message,omitempty"`
	From              *User              `json:"from,omitempty"`
	Chat              Chat               `json:"chat"`
	Date              int64              `json:"date,omitempty"`
	Text              string             `json:"text,omitempty"`
	Caption           string             `json:"caption,omitempty"`
	ReplyToMessage    *Message           `json:"reply_to_message,omitempty"`
	Photo             json.RawMessage    `json:"photo,omitempty"`
	Document          json.RawMessage    `json:"document,omitempty"`
	SuccessfulPayment *SuccessfulPayment `json:"successful_payment,omitempty"`
	RefundedPayment   *RefundedPayment   `json:"refunded_payment,omitempty"`
}

// HasMedia говорит, что в сообщении есть фото или документ.
func (m *Message) HasMedia() bool {
	return len(m.Photo) > 0 || len(m.Document) > 0
}

// CallbackQuery это нажатие инлайн-кнопки.
type CallbackQuery struct {
	ID      string   `json:"id"`
	From    User     `json:"from"`
	Message *Message `json:"message,omitempty"`
	Data    string   `json:"data,omitempty"`
}

// PreCheckoutQuery это запрос перед оплатой, на него надо ответить за 10 секунд.
type PreCheckoutQuery struct {
	ID             string `json:"id"`
	From           User   `json:"from"`
	Currency       string `json:"currency"`
	TotalAmount    int64  `json:"total_amount"`
	InvoicePayload string `json:"invoice_payload"`
}

// SuccessfulPayment это успешная оплата (звёзды или подписка на звёзды).
type SuccessfulPayment struct {
	Currency                   string `json:"currency"`
	TotalAmount                int64  `json:"total_amount"`
	InvoicePayload             string `json:"invoice_payload"`
	SubscriptionExpirationDate int64  `json:"subscription_expiration_date,omitempty"`
	IsRecurring                bool   `json:"is_recurring,omitempty"`
	IsFirstRecurring           bool   `json:"is_first_recurring,omitempty"`
	TelegramPaymentChargeID    string `json:"telegram_payment_charge_id"`
	ProviderPaymentChargeID    string `json:"provider_payment_charge_id,omitempty"`
}

// RefundedPayment это возврат платежа.
type RefundedPayment struct {
	Currency                string `json:"currency"`
	TotalAmount             int64  `json:"total_amount"`
	InvoicePayload          string `json:"invoice_payload"`
	TelegramPaymentChargeID string `json:"telegram_payment_charge_id"`
	ProviderPaymentChargeID string `json:"provider_payment_charge_id,omitempty"`
}

// ChatMember это участник чата, нам нужен статус.
type ChatMember struct {
	Status string `json:"status"`
	User   User   `json:"user"`
}

// ChatMemberUpdated это смена статуса участника, например бота в личке (заблокировал или вернулся).
type ChatMemberUpdated struct {
	Chat          Chat       `json:"chat"`
	From          User       `json:"from"`
	Date          int64      `json:"date,omitempty"`
	OldChatMember ChatMember `json:"old_chat_member"`
	NewChatMember ChatMember `json:"new_chat_member"`
}

// ForumTopic это тема форума.
type ForumTopic struct {
	ThreadID int    `json:"message_thread_id"`
	Name     string `json:"name,omitempty"`
}

// InlineKeyboardMarkup это инлайн-клавиатура.
type InlineKeyboardMarkup struct {
	InlineKeyboard [][]InlineKeyboardButton `json:"inline_keyboard"`
}

// InlineKeyboardButton это кнопка: задан один из CallbackData, URL, WebApp, CopyText.
type InlineKeyboardButton struct {
	Text         string          `json:"text"`
	CallbackData string          `json:"callback_data,omitempty"`
	URL          string          `json:"url,omitempty"`
	WebApp       *WebAppInfo     `json:"web_app,omitempty"`
	CopyText     *CopyTextButton `json:"copy_text,omitempty"`
}

// WebAppInfo это адрес мини-приложения.
type WebAppInfo struct {
	URL string `json:"url"`
}

// CopyTextButton это текст, который кнопка копирует в буфер.
type CopyTextButton struct {
	Text string `json:"text"`
}

// LabeledPrice это строка цены в счёте.
type LabeledPrice struct {
	Label  string `json:"label"`
	Amount int64  `json:"amount"`
}

// BotCommand это команда в меню бота.
type BotCommand struct {
	Command     string `json:"command"`
	Description string `json:"description"`
}
