package bot

import (
	"context"
	"errors"
	"strconv"

	"github.com/bini69-oi/worker/core"
)

// onCancelPlanAsk спрашивает, отменить ли ждущую смену тарифа.
func (b *Bot) onCancelPlanAsk(ctx context.Context, r *req, _ Data) error {
	return b.show(ctx, r, Screen{Text: b.t(r, "plan.cancel_ask", nil),
		Rows: [][]Button{{b.btn(r, "btn.yes", "pcanok"), b.btn(r, "btn.no", "sub")}}})
}

func (b *Bot) onCancelPlan(ctx context.Context, r *req, _ Data) error {
	if err := b.core.CancelPlan(ctx, r.user.ID); err != nil {
		return err
	}
	_ = b.answer(ctx, r, b.t(r, "plan.cancelled", nil))
	return b.showSub(ctx, r)
}

// onTrafficPacks показывает пакеты трафика с ценами. Без аргумента: список, с аргументом: покупка пакета.
func (b *Bot) onTrafficPacks(ctx context.Context, r *req, d Data) error {
	if gb, ok := d.Int(0); ok {
		if !b.once(r, d) {
			return b.answer(ctx, r, b.t(r, "bot.busy", nil))
		}
		if _, err := b.core.BuyTraffic(ctx, r.user.ID, gb); err != nil {
			b.forget(pressKey(r.from.ID, r.msgID, d))
			return b.addonError(ctx, r, err)
		}
		_ = b.answer(ctx, r, b.t(r, "addons.done", nil))
		return b.showSub(ctx, r)
	}
	offer, err := b.core.Addons(ctx, r.user.ID)
	if err != nil {
		return err
	}
	s := Screen{Text: b.t(r, "addons.traffic_choose", nil)}
	for _, p := range offer.Traffic {
		s.Rows = append(s.Rows, []Button{{Text: b.t(r, "btn.traffic_pack", map[string]string{
			"gb": strconv.FormatInt(p.GB, 10), "price": money(p.Price)}), Data: data("at", p.GB)}})
	}
	s.Rows = append(s.Rows, b.back(r, "sub"))
	return b.show(ctx, r, s)
}

// onDevice показывает цену устройства до конца периода и покупает после подтверждения.
func (b *Bot) onDevice(ctx context.Context, r *req, d Data) error {
	if len(d.Args) == 1 && d.Args[0] == "ok" {
		if !b.once(r, d) {
			return b.answer(ctx, r, b.t(r, "bot.busy", nil))
		}
		if _, err := b.core.BuyDevice(ctx, r.user.ID); err != nil {
			b.forget(pressKey(r.from.ID, r.msgID, d))
			return b.addonError(ctx, r, err)
		}
		_ = b.answer(ctx, r, b.t(r, "addons.done", nil))
		return b.showSub(ctx, r)
	}
	offer, err := b.core.Addons(ctx, r.user.ID)
	if err != nil {
		return err
	}
	if offer.DevicePrice <= 0 {
		return core.ErrAddonOff
	}
	return b.show(ctx, r, Screen{Text: b.t(r, "addons.device_ask", map[string]string{"price": money(offer.DevicePrice)}),
		Rows: [][]Button{{b.btn(r, "btn.yes", data("ad", "ok")), b.btn(r, "btn.no", "sub")}}})
}

// addonError: не хватило денег на докупку, предлагаем пополнить на разницу. Остальное как обычная ошибка.
func (b *Bot) addonError(ctx context.Context, r *req, err error) error {
	var need *core.NeedMoney
	if errors.As(err, &need) {
		return b.show(ctx, r, Screen{Text: b.t(r, "addons.need", map[string]string{"missing": money(need.Missing)}),
			Rows: [][]Button{{{Text: b.t(r, "btn.topup_missing", map[string]string{"missing": money(need.Missing)}),
				Data: data("tp", need.Missing)}}, b.back(r, "sub")}})
	}
	return err
}
