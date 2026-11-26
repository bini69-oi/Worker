package bot

import "context"

// inputFn обрабатывает текст, которого бот ждал после кнопки.
type inputFn func(ctx context.Context, r *req, w wait, text string) error

// onInput отдаёт текст обработчику ожидания. Неизвестное ожидание показывает меню.
func (b *Bot) onInput(ctx context.Context, r *req, w wait, text string) error {
	if fn, ok := b.inputs[w.kind]; ok {
		return fn(ctx, r, w, text)
	}
	return b.showMenu(ctx, r)
}

// expect ставит ожидание текста от абонента.
func (b *Bot) expect(r *req, kind waitKind, arg string) {
	b.waits.set(r.from.ID, kind, arg, b.now())
}
