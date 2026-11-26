package bot

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/bini69-oi/worker/core"
	"github.com/bini69-oi/worker/tg"
)

// maxTopupRub это предел своей суммы пополнения в рублях: защита от опечатки «1000000000».
const maxTopupRub = 1_000_000

// onTopup показывает пополнение. Без суммы: готовые суммы и «Своя сумма». С суммой: кассы.
func (b *Bot) onTopup(ctx context.Context, r *req, d Data) error {
	if amount, ok := d.Int(0); ok {
		return b.showGateways(ctx, r, amount)
	}
	s := Screen{Text: b.t(r, "topup.amount", nil)}
	var row []Button
	for _, p := range strings.Split(b.cfg.String("pay.presets"), ",") {
		rub, err := strconv.ParseInt(strings.TrimSpace(p), 10, 64)
		if err != nil || rub <= 0 || rub > maxTopupRub {
			continue
		}
		row = append(row, Button{Text: money(rub * 100), Data: data("tp", rub*100)})
		if len(row) == 2 {
			s.Rows, row = append(s.Rows, row), nil
		}
	}
	if len(row) > 0 {
		s.Rows = append(s.Rows, row)
	}
	s.Rows = append(s.Rows, []Button{b.btn(r, "btn.own_amount", "tpown")}, b.back(r, "bal"))
	return b.show(ctx, r, s)
}

func (b *Bot) onOwnAmount(ctx context.Context, r *req, _ Data) error {
	b.expect(r, waitAmount, "")
	return b.show(ctx, r, Screen{Text: b.t(r, "topup.own", map[string]string{"max": money(maxTopupRub * 100)}),
		Rows: [][]Button{b.back(r, "tp")}})
}

// onAmountInput принимает свою сумму: только целое число рублей от 1 до предела.
func (b *Bot) onAmountInput(ctx context.Context, r *req, _ wait, text string) error {
	rub, err := strconv.ParseInt(text, 10, 64)
	if err != nil || rub < 1 || rub > maxTopupRub || text != strconv.FormatInt(rub, 10) {
		b.expect(r, waitAmount, "")
		return b.say(ctx, r.chatID, b.t(r, "topup.bad_amount", map[string]string{"max": money(maxTopupRub * 100)}))
	}
	r.msgID = 0 // ответ на текст идёт новым сообщением
	return b.showGateways(ctx, r, rub*100)
}

// showGateways показывает кассы, которые ядро считает доступными, для суммы в копейках.
func (b *Bot) showGateways(ctx context.Context, r *req, amount int64) error {
	if amount <= 0 || amount > maxTopupRub*100 {
		return core.ErrInvalid
	}
	s := Screen{Text: b.t(r, "topup.gateways", map[string]string{"amount": money(amount)})}
	for _, g := range b.core.Gateways() {
		s.Rows = append(s.Rows, []Button{{Text: g.Title, Data: data("tpg", g.Name, amount)}})
	}
	if len(s.Rows) == 0 {
		s.Text = b.t(r, "topup.no_gateways", nil)
	}
	s.Rows = append(s.Rows, b.back(r, "tp"))
	return b.show(ctx, r, s)
}

// onGateway создаёт платёж в кассе. Stars: счёт Telegram прямо в чате. Остальные: кнопка со ссылкой
// и «Проверить оплату». Ручная касса: реквизиты текстом и та же проверка.
func (b *Bot) onGateway(ctx context.Context, r *req, d Data) error {
	amount, ok := d.Int(1)
	if len(d.Args) != 2 || !ok {
		return core.ErrInvalid
	}
	if !b.once(r, d) {
		return b.answer(ctx, r, b.t(r, "bot.busy", nil))
	}
	gw := d.Args[0]
	p, inv, err := b.core.CreatePayment(ctx, r.user.ID, gw, amount)
	var le *core.LimitError
	if errors.As(err, &le) {
		return b.say(ctx, r.chatID, b.t(r, "topup.limits", map[string]string{"min": b.limit(r, le.Min), "max": b.limit(r, le.Max)}))
	}
	if err != nil {
		return err
	}
	vars := map[string]string{"amount": money(amount)}
	if inv.ExtCurrency == "XTR" {
		_, err := b.api.SendInvoice(ctx, tg.InvoiceParams{ChatID: r.chatID, Title: b.t(r, "stars.title", vars),
			Description: b.t(r, "stars.desc", vars), Payload: payPayload(p.ID), Currency: "XTR",
			Prices: []tg.LabeledPrice{{Label: b.t(r, "stars.title", vars), Amount: inv.ExtAmount}}})
		return err
	}
	s := Screen{Text: b.t(r, "topup.link", vars)}
	if inv.Text != "" {
		vars["details"] = inv.Text
		s.Text = b.t(r, "topup.manual", vars)
	}
	if inv.URL != "" {
		s.Rows = append(s.Rows, []Button{{Text: b.t(r, "btn.pay", vars), URL: inv.URL}})
	}
	s.Rows = append(s.Rows, []Button{b.btn(r, "btn.check_pay", data("pchk", p.ID))}, b.back(r, "m"))
	return b.show(ctx, r, s)
}

// Данные счёта Stars: p:<платёж> для пополнения, s:<тариф> для автоподписки.
func payPayload(id int64) string    { return "p:" + strconv.FormatInt(id, 10) }
func subPayload(tariff int64) string { return "s:" + strconv.FormatInt(tariff, 10) }

// parsePayload разбирает данные счёта. Это строка, которую Telegram вернул нам, но проверяем как чужую.
func parsePayload(s string) (kind string, id int64, ok bool) {
	kind, num, found := strings.Cut(s, ":")
	if !found || (kind != "p" && kind != "s") {
		return "", 0, false
	}
	id, err := strconv.ParseInt(num, 10, 64)
	return kind, id, err == nil && id > 0
}

// limit пишет предел кассы, 0 значит без предела.
func (b *Bot) limit(r *req, kop int64) string {
	if kop <= 0 {
		return b.t(r, "sub.unlimited", nil)
	}
	return money(kop)
}
