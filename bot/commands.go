package bot

import (
	"context"
	"strconv"
	"strings"
)

// maxStartArg это предел параметра /start: Telegram пускает до 64 символов.
const maxStartArg = 64

// onCommand разбирает команду абонента.
func (b *Bot) onCommand(ctx context.Context, r *req, cmd, arg string) error {
	switch cmd {
	case "start":
		return b.onStart(ctx, r, arg)
	case "buy":
		return b.showTariffs(ctx, r)
	case "sub":
		return b.showSub(ctx, r)
	case "balance":
		return b.showBalance(ctx, r)
	case "help":
		return b.showHelp(ctx, r)
	}
	return b.showMenu(ctx, r)
}

// onStart это вход в бота, в том числе по ссылке. Реферальный код уже учтён при регистрации.
func (b *Bot) onStart(ctx context.Context, r *req, arg string) error {
	kind, val := startArg(arg)
	switch kind {
	case "promo":
		if err := b.applyPromo(ctx, r, val); err != nil {
			_ = b.fail(ctx, r, err)
			return b.showMenu(ctx, r)
		}
		return nil
	case "paid":
		id, _ := strconv.ParseInt(val, 10, 64)
		return b.showPaid(ctx, r, id)
	}
	return b.showMenu(ctx, r)
}

// startArg разбирает параметр /start: ref_<код>, promo_<код> или paid_<число>.
// Это чужой ввод: всё, что не подходит под эти виды, игнорируется.
func startArg(arg string) (kind, val string) {
	if arg == "" || len(arg) > maxStartArg {
		return "", ""
	}
	kind, val, ok := strings.Cut(arg, "_")
	if !ok || !word(val, 1, 32, true) {
		return "", ""
	}
	switch kind {
	case "ref", "promo":
		return kind, val
	case "paid":
		if n, err := strconv.ParseInt(val, 10, 64); err == nil && n > 0 {
			return kind, val
		}
	}
	return "", ""
}

// refFromStart достаёт реферальный код из текста «/start ref_…». Пусто, если его нет.
func refFromStart(text string) string {
	cmd, arg, ok := command(text)
	if !ok || cmd != "start" {
		return ""
	}
	if kind, val := startArg(arg); kind == "ref" {
		return val
	}
	return ""
}
