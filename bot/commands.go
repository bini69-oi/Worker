package bot

import "context"

// onCommand разбирает команду абонента.
func (b *Bot) onCommand(ctx context.Context, r *req, cmd, arg string) error {
	return b.showMenu(ctx, r)
}
