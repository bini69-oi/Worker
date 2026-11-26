package bot

import (
	"context"

	"github.com/bini69-oi/worker/apps"
)

// showConnect показывает подключение: одно-два приложения, где скачать и кнопку «Добавить подписку».
// Своя схема приложения (happ://) текстом не показывается: Telegram не открывает её кнопкой,
// поэтому кнопка ведёт на /dl, а сервер переводит браузер в приложение.
func (b *Bot) showConnect(ctx context.Context, r *req) error {
	sub, hasSub, err := b.sub(ctx, r)
	if err != nil {
		return err
	}
	if !hasSub {
		return b.showSub(ctx, r) // там предложат пробный период или покупку
	}
	if sub.SubURL == "" {
		return b.show(ctx, r, Screen{Text: b.t(r, "sub.pending", nil), Rows: [][]Button{{b.btn(r, "btn.check", "con")}, b.back(r, "m")}})
	}
	s := Screen{Text: b.t(r, "connect.title", nil)}
	tok := apps.Token(b.dlKey, r.user.ID, b.now())
	for _, a := range apps.List(b.cfg.String("connect.apps")) {
		var stores []Button
		for _, p := range apps.Platforms {
			if url, ok := a.Stores[p]; ok {
				stores = append(stores, Button{Text: b.t(r, "btn.store", map[string]string{"app": a.Name,
					"platform": b.t(r, "platform."+string(p), nil)}), URL: url})
			}
		}
		s.Rows = append(s.Rows, stores,
			[]Button{{Text: b.t(r, "btn.add", map[string]string{"app": a.Name}), URL: b.url + "/dl/" + a.ID + "/" + tok}})
	}
	s.Rows = append(s.Rows, []Button{{Text: b.t(r, "btn.copy", nil), Copy: sub.SubURL}}, b.back(r, "m"))
	return b.show(ctx, r, s)
}
