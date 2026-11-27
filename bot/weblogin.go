package bot

import (
	"context"

	"github.com/bini69-oi/worker/core"
)

// showWebLogin отвечает на ссылку входа на сайт t.me/<бот>?start=web_<nonce>: откуда входят и три кода.
// Нажать нужно тот, что показывает сайт. Устаревшую и чужую ссылку ядро отклоняет, абонент видит код ошибки.
func (b *Bot) showWebLogin(ctx context.Context, r *req, nonce string) error {
	p, err := b.core.WebLoginOpen(ctx, nonce, r.from.ID)
	if err != nil {
		return err
	}
	var row []Button
	for _, code := range p.Codes {
		row = append(row, Button{Text: code, Data: data("wl", p.ID, code)})
	}
	r.msgID = 0 // ссылка открывается командой: ответ идёт новым сообщением
	return b.show(ctx, r, Screen{
		Text: b.t(r, "bot.web_login", map[string]string{"ip": p.IP, "agent": p.Agent}),
		Rows: [][]Button{row, {b.btn(r, "btn.not_me", data("wl", p.ID, "no"))}},
	})
}

// onWebLogin принимает нажатие кода или «Это не я» и показывает итог на месте кнопок.
func (b *Bot) onWebLogin(ctx context.Context, r *req, d Data) error {
	id, ok := d.Int(0)
	if !ok || len(d.Args) != 2 {
		return core.ErrInvalid
	}
	yes, err := b.core.WebLoginAnswer(ctx, id, r.from.ID, d.Args[1])
	if err != nil {
		return err
	}
	key := "bot.web_login_no"
	if yes {
		key = "bot.web_login_ok"
	}
	return b.show(ctx, r, Screen{Text: b.t(r, key, nil)})
}
