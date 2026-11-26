package bot

import (
	"context"

	"github.com/bini69-oi/worker/tg"
)

// Setup ставит список команд на каждом языке бота и кнопку меню. Зовётся при старте процесса.
// Если включён Mini App, кнопка меню открывает его, иначе это обычный список команд.
func (b *Bot) Setup(ctx context.Context) error {
	for _, lang := range b.tx.Langs() {
		r := &req{lang: lang}
		cmds := []tg.BotCommand{
			{Command: "start", Description: b.t(r, "cmd.start", nil)},
			{Command: "buy", Description: b.t(r, "cmd.buy", nil)},
			{Command: "sub", Description: b.t(r, "cmd.sub", nil)},
			{Command: "balance", Description: b.t(r, "cmd.balance", nil)},
			{Command: "help", Description: b.t(r, "cmd.help", nil)},
		}
		code := lang
		if lang == b.lang("") {
			code = "" // язык по умолчанию ставится для всех, у кого нет своего
		}
		if err := b.api.SetMyCommands(ctx, cmds, code); err != nil {
			return err
		}
	}
	if b.cfg.Bool("app.miniapp") && b.url != "" {
		r := &req{lang: b.lang("")}
		return b.api.SetChatMenuButton(ctx, 0, b.t(r, "btn.app", nil), b.url+"/app/")
	}
	return b.api.SetChatMenuButton(ctx, 0, "", "")
}
