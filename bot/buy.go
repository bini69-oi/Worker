package bot

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/bini69-oi/worker/core"
)

// onceTTL это сколько бот помнит нажатую кнопку оплаты: второй клик по той же кнопке не платит второй раз.
const onceTTL = 2 * time.Minute

// once пропускает кнопку один раз. Ядро и так не спишет за одну покупку дважды, но второй клик по старой
// кнопке «Оплатить» продлил бы подписку ещё раз: это отдельная покупка с точки зрения ядра.
// Ключ: кто нажал, в каком сообщении и что. Если действие не удалось, ключ снимает forget.
func (b *Bot) once(r *req, d Data) bool { return b.onceKey(pressKey(r.from.ID, r.msgID, d)) }

func pressKey(who int64, msg int, d Data) string {
	return itoa(who) + ":" + strconv.Itoa(msg) + ":" + d.Act + ":" + strings.Join(d.Args, ":")
}

func (b *Bot) onceKey(key string) bool {
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

// forget снимает отметку нажатия: действие не удалось, повторный клик должен сработать.
func (b *Bot) forget(key string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.pressed, key)
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
	id, days, ok := d.pair()
	if !ok {
		return core.ErrInvalid
	}
	t, err := b.core.Tariff(ctx, id)
	if err != nil {
		return err
	}
	q, err := b.core.Quote(ctx, r.user.ID, id, int(days))
	if err != nil {
		return err
	}
	vars := map[string]string{"tariff": t.Name, "days": strconv.FormatInt(days, 10), "price": money(q.Price),
		"balance": money(q.Balance), "missing": money(q.Missing), "starts": date(q.StartsAt)}
	confirm := "buy.confirm"
	if q.Queued {
		confirm = "buy.queue_confirm" // другой тариф при живой подписке: начнётся, когда закончится текущий
	}
	if q.Missing == 0 {
		return b.show(ctx, r, Screen{Text: b.t(r, confirm, vars), Rows: [][]Button{
			{b.btn(r, "btn.pay_balance", data("bok", id, days))}, b.back(r, data("bt", id))}})
	}
	if err := b.core.RememberBuy(ctx, r.user.ID, id, int(days)); err != nil {
		return err
	}
	return b.show(ctx, r, Screen{Text: b.t(r, "buy.need", vars), Rows: [][]Button{
		{{Text: b.t(r, "btn.topup_missing", vars), Data: data("tp", q.Missing)}}, b.back(r, data("bt", id))}})
}

// onBuy покупает с баланса. Повторный клик по той же кнопке получает «уже обрабатываем».
func (b *Bot) onBuy(ctx context.Context, r *req, d Data) error {
	id, days, ok := d.pair()
	if !ok {
		return core.ErrInvalid
	}
	if !b.once(r, d) {
		return b.answer(ctx, r, b.t(r, "bot.busy", nil))
	}
	sub, err := b.core.Buy(ctx, r.user.ID, id, int(days))
	if err != nil {
		b.forget(pressKey(r.from.ID, r.msgID, d))
	}
	var need *core.NeedMoney
	if errors.As(err, &need) {
		return b.onPeriod(ctx, r, d) // баланс успел уменьшиться: покажем недостачу
	}
	if err != nil {
		return err
	}
	if sub.NextTariffID != 0 {
		t, err := b.core.Tariff(ctx, sub.NextTariffID)
		if err != nil {
			return err
		}
		return b.show(ctx, r, Screen{Text: b.t(r, "buy.queued", map[string]string{"tariff": t.Name, "date": date(sub.ExpiresAt)}),
			Rows: [][]Button{{b.btn(r, "btn.sub", "sub")}, b.back(r, "m")}})
	}
	return b.show(ctx, r, Screen{Text: b.t(r, "buy.done", map[string]string{"date": date(sub.ExpiresAt)}),
		Rows: [][]Button{{b.btn(r, "btn.connect", "con")}, b.back(r, "m")}})
}
