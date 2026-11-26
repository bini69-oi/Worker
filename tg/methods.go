package tg

import "context"

// SendParams это параметры sendMessage.
type SendParams struct {
	ChatID         int64
	ThreadID       int
	Text           string
	ParseMode      string
	Markup         *InlineKeyboardMarkup
	ReplyTo        int
	DisablePreview bool
}

type linkPreview struct {
	IsDisabled bool `json:"is_disabled"`
}

type replyParams struct {
	MessageID                int  `json:"message_id"`
	AllowSendingWithoutReply bool `json:"allow_sending_without_reply"`
}

type sendMessageReq struct {
	ChatID      int64                 `json:"chat_id"`
	ThreadID    int                   `json:"message_thread_id,omitempty"`
	Text        string                `json:"text"`
	ParseMode   string                `json:"parse_mode,omitempty"`
	Markup      *InlineKeyboardMarkup `json:"reply_markup,omitempty"`
	ReplyParams *replyParams          `json:"reply_parameters,omitempty"`
	Preview     *linkPreview          `json:"link_preview_options,omitempty"`
}

func reply(id int) *replyParams {
	if id == 0 {
		return nil
	}
	return &replyParams{MessageID: id, AllowSendingWithoutReply: true}
}

// GetMe возвращает данные бота.
func (c *Client) GetMe(ctx context.Context) (User, error) {
	var u User
	err := c.call(ctx, "getMe", nil, &u)
	return u, err
}

// SetWebhook ставит вебхук с секретом.
func (c *Client) SetWebhook(ctx context.Context, url, secret string, allowedUpdates []string) error {
	return c.call(ctx, "setWebhook", map[string]any{
		"url":             url,
		"secret_token":    secret,
		"allowed_updates": allowedUpdates,
	}, nil)
}

// DeleteWebhook снимает вебхук.
func (c *Client) DeleteWebhook(ctx context.Context) error {
	return c.call(ctx, "deleteWebhook", nil, nil)
}

// GetUpdates забирает обновления длинным опросом, timeout в секундах.
func (c *Client) GetUpdates(ctx context.Context, offset int64, timeout int) ([]Update, error) {
	var ups []Update
	err := c.call(ctx, "getUpdates", map[string]any{"offset": offset, "timeout": timeout}, &ups)
	return ups, err
}

// SendMessage отправляет текст.
func (c *Client) SendMessage(ctx context.Context, p SendParams) (Message, error) {
	req := sendMessageReq{
		ChatID: p.ChatID, ThreadID: p.ThreadID, Text: p.Text, ParseMode: p.ParseMode,
		Markup: p.Markup, ReplyParams: reply(p.ReplyTo),
	}
	if p.DisablePreview {
		req.Preview = &linkPreview{IsDisabled: true}
	}
	var m Message
	err := c.call(ctx, "sendMessage", req, &m)
	return m, err
}

// EditParams это параметры editMessageText.
type EditParams struct {
	ChatID         int64
	MessageID      int
	Text           string
	ParseMode      string
	Markup         *InlineKeyboardMarkup
	DisablePreview bool
}

// EditMessageText правит текст и клавиатуру сообщения.
func (c *Client) EditMessageText(ctx context.Context, p EditParams) error {
	req := map[string]any{"chat_id": p.ChatID, "message_id": p.MessageID, "text": p.Text}
	if p.ParseMode != "" {
		req["parse_mode"] = p.ParseMode
	}
	if p.Markup != nil {
		req["reply_markup"] = p.Markup
	}
	if p.DisablePreview {
		req["link_preview_options"] = linkPreview{IsDisabled: true}
	}
	return c.call(ctx, "editMessageText", req, nil)
}

// AnswerCallbackQuery отвечает на нажатие кнопки.
func (c *Client) AnswerCallbackQuery(ctx context.Context, id, text string, showAlert bool) error {
	req := map[string]any{"callback_query_id": id}
	if text != "" {
		req["text"] = text
	}
	if showAlert {
		req["show_alert"] = true
	}
	return c.call(ctx, "answerCallbackQuery", req, nil)
}

// CopyMessage копирует сообщение и возвращает id копии.
func (c *Client) CopyMessage(ctx context.Context, toChat int64, thread int, fromChat int64, msgID, replyTo int) (int, error) {
	req := map[string]any{"chat_id": toChat, "from_chat_id": fromChat, "message_id": msgID}
	if thread != 0 {
		req["message_thread_id"] = thread
	}
	if rp := reply(replyTo); rp != nil {
		req["reply_parameters"] = rp
	}
	var res struct {
		MessageID int `json:"message_id"`
	}
	err := c.call(ctx, "copyMessage", req, &res)
	return res.MessageID, err
}

// SetMyCommands задаёт меню команд для языка (пустой lang: для всех).
func (c *Client) SetMyCommands(ctx context.Context, commands []BotCommand, lang string) error {
	req := map[string]any{"commands": commands}
	if lang != "" {
		req["language_code"] = lang
	}
	return c.call(ctx, "setMyCommands", req, nil)
}

// SetChatMenuButton ставит кнопку меню: мини-приложение, если задан webAppURL, иначе обычную.
// chatID 0 значит кнопка по умолчанию для всех.
func (c *Client) SetChatMenuButton(ctx context.Context, chatID int64, text, webAppURL string) error {
	btn := map[string]any{"type": "default"}
	if webAppURL != "" {
		btn = map[string]any{"type": "web_app", "text": text, "web_app": WebAppInfo{URL: webAppURL}}
	}
	req := map[string]any{"menu_button": btn}
	if chatID != 0 {
		req["chat_id"] = chatID
	}
	return c.call(ctx, "setChatMenuButton", req, nil)
}

// CreateForumTopic создаёт тему и возвращает её message_thread_id.
func (c *Client) CreateForumTopic(ctx context.Context, chat int64, name string) (int, error) {
	var t ForumTopic
	err := c.call(ctx, "createForumTopic", map[string]any{"chat_id": chat, "name": name}, &t)
	return t.ThreadID, err
}

// GetChatMember возвращает статус участника чата.
// chat это @имя канала или его числовой id строкой: Telegram принимает оба.
func (c *Client) GetChatMember(ctx context.Context, chat string, user int64) (string, error) {
	var m ChatMember
	err := c.call(ctx, "getChatMember", map[string]any{"chat_id": chat, "user_id": user}, &m)
	return m.Status, err
}

// LeaveChat выходит из чата.
func (c *Client) LeaveChat(ctx context.Context, chat int64) error {
	return c.call(ctx, "leaveChat", map[string]any{"chat_id": chat}, nil)
}

// EditMessageReplyMarkup меняет только кнопки сообщения, текст и его разметка остаются.
func (c *Client) EditMessageReplyMarkup(ctx context.Context, chatID int64, messageID int, markup *InlineKeyboardMarkup) error {
	req := map[string]any{"chat_id": chatID, "message_id": messageID}
	if markup != nil {
		req["reply_markup"] = markup
	}
	return c.call(ctx, "editMessageReplyMarkup", req, nil)
}
