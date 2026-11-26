package bot

import (
	"context"

	"github.com/bini69-oi/worker/tg"
)

// Tokens проверяет новый токен бота и ставит на него вебхук. Это реализация core.TelegramAPI.
type Tokens struct {
	BaseURL string        // адрес Bot API, пусто значит боевой
	Hook    string        // адрес вебхука
	Secret  func() string // секрет вебхука текущего процесса
}

func (t Tokens) client(token string) *tg.Client { return &tg.Client{Token: token, BaseURL: t.BaseURL} }

// CheckToken спрашивает Telegram getMe с новым токеном.
func (t Tokens) CheckToken(ctx context.Context, token string) error {
	_, err := t.client(token).GetMe(ctx)
	return err
}

// SetWebhook ставит вебхук на новый токен с секретом текущего процесса.
func (t Tokens) SetWebhook(ctx context.Context, token string) error {
	return t.client(token).SetWebhook(ctx, t.Hook, t.Secret(), Allowed)
}

// Allowed это виды обновлений, которые нужны боту. Остальные Telegram не шлёт.
var Allowed = []string{"message", "callback_query", "pre_checkout_query", "my_chat_member"}
