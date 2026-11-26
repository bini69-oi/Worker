package bot

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/bini69-oi/worker/core"
)

// onceTTL это сколько бот помнит нажатую кнопку оплаты: второй клик по той же кнопке не платит второй раз.
const onceTTL = 2 * time.Minute

// once пропускает кнопку один раз. Ядро и так не спишет за одну покупку дважды, но второй клик по старой
// кнопке «Оплатить» продлил бы подписку ещё раз: это отдельная покупка с точки зрения ядра.
func (b *Bot) once(r *req, d Data) bool {
	key := strconv.FormatInt(r.from.ID, 10) + ":" + strconv.Itoa(r.msgID) + ":" + data(d.Act, toArgs(d.Args)...)
	b.mu.Lock()
	defer b.mu.Unlock()
	now := b.now()
	for k, at := range b.pressed {
		if now.Sub(at) > onceTTL {
			delete(b.pressed, k)
		}
	}
	if _, seen := b.pressed[key]; seen {
		return false
	}
	b.pressed[key] = now
	return true
}

func toArgs(ss []string) []any {
	out := make([]any, len(ss))
	for i, s := range ss {
		out[i] = s
	}
	return out
}

// showTariffs показывает тарифы, которые продаются.
func (b *Bot) showTariffs(ctx context.Context, r *req) error {
	list, err := b.core.Tariffs(ctx)
	if err != nil {
		return err
	}
	s := Screen{Text: b.t(r, "buy.tariffs", nil)}
	if len(list) == 0 {
		s.Text = b.t(r, "buy.no_tariffs", nil)
	}
	for _, t := range list {
		s.Rows = append(s.Rows, []Button{{Text: t.Name, Data: data("bt", t.ID)}})
	}
	s.Rows = append(s.Rows, b.back(r, "m"))
	return b.show(ctx, r, s)
}

// onTariff показывает сроки тарифа с ценами.
func (b *Bot) onTariff(ctx context.Context, r *req, d Data) error {
	id, ok := d.Int(0)
	if !ok {
		return core.ErrInvalid
	}
	t, err := b.core.Tariff(ctx, id)
	if err != nil {
		return err
	}
	s := Screen{Text: b.t(r, "buy.periods", map[string]string{"tariff": t.Name, "traffic": b.gb(r, t.TrafficBytes), "devices": b.count(r, t.Devices)})}
	for _, p := range t.Periods {
		s.Rows = append(s.Rows, []Button{{Text: b.t(r, "btn.period", map[string]string{"days": strconv.Itoa(p.Days), "price": money(p.Price)}),
			Data: data("bp", t.ID, p.Days)}})
	}
	if b.core.StarsFor30(t) > 0 {
		s.Rows = append(s.Rows, []Button{b.btn(r, "btn.stars_sub", data("ss", t.ID))})
	}
	s.Rows = append(s.Rows, b.back(r, "buy"))
	return b.show(ctx, r, s)
}

// onPeriod показывает подтверждение с ценой. Денег не хватает: недостача цифрами и пополнение на разницу,
// а выбор запоминается в корзине ядра и купится сам после оплаты.
func (b *Bot) onPeriod(ctx context.Context, r *req, d Data) error {
	id, ok1 := d.Int(0)
	days, ok2 := d.Int(1)
	if !ok1 || !ok2 {
		return core.ErrInvalid
	}
	t, err := b.core.Tariff(ctx, id)
	if err != nil {
		return err
	}
	q, err := b.core.Quote(ctx, r.user.ID, id, int(days))
	if errors.Is(err, core.ErrOtherTariff) {
		return b.otherTariff(ctx, r)
	}
	if err != nil {
		return err
	}
	vars := map[string]string{"tariff": t.Name, "days": strconv.FormatInt(days, 10), "price": money(q.Price),
		"balance": money(q.Balance), "missing": money(q.Missing)}
	if q.Missing == 0 {
		return b.show(ctx, r, Screen{Text: b.t(r, "buy.confirm", vars), Rows: [][]Button{
			{b.btn(r, "btn.pay_balance", data("bok", id, days))}, b.back(r, data("bt", id))}})
	}
	if err := b.core.RememberBuy(ctx, r.user.ID, id, int(days)); err != nil {
		return err
	}
	return b.show(ctx, r, Screen{Text: b.t(r, "buy.need", vars), Rows: [][]Button{
		{{Text: b.t(r, "btn.topup_missing", vars), Data: data("tp", q.Missing)}}, b.back(r, data("bt", id))}})
}

// otherTariff объясняет, что смена тарифа пока недоступна, и предлагает продлить текущий.
func (b *Bot) otherTariff(ctx context.Context, r *req) error {
	sub, _, err := b.sub(ctx, r)
	if err != nil {
		return err
	}
	return b.show(ctx, r, Screen{Text: b.t(r, core.ErrOtherTariff.Key, nil), Rows: [][]Button{
		{b.btn(r, "btn.extend", data("bt", sub.TariffID))}, b.back(r, "buy")}})
}

// onBuy покупает с баланса. Повторный клик по той же кнопке получает «уже обрабатываем».
func (b *Bot) onBuy(ctx context.Context, r *req, d Data) error {
	id, ok1 := d.Int(0)
	days, ok2 := d.Int(1)
	if !ok1 || !ok2 {
		return core.ErrInvalid
	}
	if !b.once(r, d) {
		return b.answer(ctx, r, b.t(r, "bot.busy", nil))
	}
	sub, err := b.core.Buy(ctx, r.user.ID, id, int(days))
	var need *core.NeedMoney
	if errors.As(err, &need) {
		return b.onPeriod(ctx, r, d) // баланс успел уменьшиться: покажем недостачу
	}
	if err != nil {
		return err
	}
	return b.show(ctx, r, Screen{Text: b.t(r, "buy.done", map[string]string{"date": date(sub.ExpiresAt)}),
		Rows: [][]Button{{b.btn(r, "btn.connect", "con")}, b.back(r, "m")}})
}
