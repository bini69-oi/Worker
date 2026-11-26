package bot

import "context"

// register связывает действия кнопок с обработчиками.
func (b *Bot) register() {
	b.acts["m"] = func(ctx context.Context, r *req, _ Data) error { return b.showMenu(ctx, r) }
}

// showMenu показывает главное меню.
func (b *Bot) showMenu(ctx context.Context, r *req) error {
	return b.show(ctx, r, b.menu(r))
}

func (b *Bot) menu(r *req) Screen {
	return Screen{Text: b.t(r, "bot.menu", nil)}
}
