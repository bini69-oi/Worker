package bot

import (
	"context"
	"strconv"
	"strings"

	"github.com/bini69-oi/worker/core"
)

// historyLines это сколько последних движений денег показывает экран баланса.
const historyLines = 10

// showBalance показывает баланс и последние движения денег.
func (b *Bot) showBalance(ctx context.Context, r *req) error {
	list, err := b.core.History(ctx, r.user.ID, historyLines)
	if err != nil {
		return err
	}
	history := ""
	if len(list) > 0 {
		lines := make([]string, 0, len(list))
		for _, e := range list {
			amount := money(e.Amount)
			if e.Amount > 0 {
				amount = "+" + amount
			}
			lines = append(lines, b.t(r, "balance.item", map[string]string{"date": date(e.CreatedAt), "amount": amount,
				"reason": b.t(r, "reason."+string(e.Reason), nil)}))
		}
		history = b.t(r, "balance.history", map[string]string{"list": strings.Join(lines, "\n")})
	}
	return b.show(ctx, r, Screen{Text: b.t(r, "balance.show", map[string]string{"balance": money(r.user.Balance), "history": history}),
		Rows: [][]Button{{b.btn(r, "btn.topup", "tp")}, b.back(r, "m")}})
}

// showRef показывает реферальную ссылку и цифры приглашений.
func (b *Bot) showRef(ctx context.Context, r *req) error {
	st, err := b.core.RefStats(ctx, r.user.ID)
	if err != nil {
		return err
	}
	link := "https://t.me/" + b.botName() + "?start=ref_" + r.user.RefCode
	return b.show(ctx, r, Screen{Text: b.t(r, "ref.show", map[string]string{"link": link,
		"invited": strconv.Itoa(st.Invited), "bought": strconv.Itoa(st.Bought), "earned": money(st.Earned)}),
		Rows: [][]Button{{{Text: b.t(r, "btn.copy_ref", nil), Copy: link}}, b.back(r, "m")}})
}

func (b *Bot) onPromoAsk(ctx context.Context, r *req, _ Data) error {
	b.expect(r, waitPromo, "")
	return b.show(ctx, r, Screen{Text: b.t(r, "promo.ask", nil), Rows: [][]Button{b.back(r, "m")}})
}

func (b *Bot) onPromoInput(ctx context.Context, r *req, _ wait, text string) error {
	return b.applyPromo(ctx, r, text)
}

// applyPromo применяет код и пишет, что он дал. Зовут ввод кода и ссылка promo_.
func (b *Bot) applyPromo(ctx context.Context, r *req, code string) error {
	p, err := b.core.ApplyPromo(ctx, r.user.ID, code)
	if err != nil {
		return err
	}
	text := b.t(r, "promo.days", map[string]string{"days": strconv.FormatInt(p.Value, 10)})
	if p.Kind == core.PromoMoney {
		text = b.t(r, "promo.money", map[string]string{"amount": money(p.Value)})
	}
	if err := b.say(ctx, r.chatID, text); err != nil {
		return err
	}
	if r.user, err = b.core.UserByTg(ctx, r.from.ID); err != nil {
		return err
	}
	return b.showMenu(ctx, r)
}

// showLangs показывает языки, на которых у бота есть тексты.
func (b *Bot) showLangs(ctx context.Context, r *req) error {
	s := Screen{Text: b.t(r, "lang.choose", nil)}
	var row []Button
	for _, l := range b.tx.Langs() {
		row = append(row, Button{Text: strings.ToUpper(l), Data: data("ln", l)})
	}
	s.Rows = append(s.Rows, row, b.back(r, "m"))
	return b.show(ctx, r, s)
}

func (b *Bot) onLang(ctx context.Context, r *req, d Data) error {
	if len(d.Args) != 1 {
		return core.ErrInvalid
	}
	if err := b.core.SetLang(ctx, r.user.ID, d.Args[0]); err != nil {
		return err
	}
	r.lang = d.Args[0]
	return b.showMenu(ctx, r)
}
