package bot

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"strings"

	"github.com/bini69-oi/worker/core"
)

// showSub показывает подписку: тариф, срок, лимиты, ссылку и действия.
func (b *Bot) showSub(ctx context.Context, r *req) error {
	sub, hasSub, err := b.sub(ctx, r)
	if err != nil {
		return err
	}
	if !hasSub {
		s := Screen{Text: b.t(r, "sub.none", nil)}
		if b.trialOpen(r, false) {
			s.Rows = append(s.Rows, []Button{b.btn(r, "btn.trial", "trial")})
		}
		s.Rows = append(s.Rows, []Button{b.btn(r, "btn.buy", "buy")}, b.back(r, "m"))
		return b.show(ctx, r, s)
	}
	name, traffic, devices := b.t(r, "sub.trial_name", nil), b.t(r, "sub.unlimited", nil), b.t(r, "sub.unlimited", nil)
	if !sub.Trial {
		t, err := b.core.Tariff(ctx, sub.TariffID)
		if err != nil {
			return err
		}
		name, traffic, devices = t.Name, b.gb(r, t.TrafficBytes), b.count(r, t.Devices)
		if t.TrafficBytes > 0 { // с докупленным
			traffic = b.gb(r, t.TrafficBytes+sub.ExtraTrafficBytes)
		}
		if t.Devices > 0 {
			devices = b.count(r, t.Devices+sub.ExtraDevices)
		}
	}
	auto := b.t(r, "sub.off", nil)
	if sub.AutoRenew {
		auto = b.t(r, "sub.on", nil)
	}
	s := Screen{Text: b.t(r, "sub.show", map[string]string{"tariff": name, "status": b.status(r, sub, true),
		"traffic": traffic, "devices": devices, "autorenew": auto})}
	if sub.SubURL == "" {
		s.Text += "\n\n" + b.t(r, "sub.pending", nil)
	} else {
		s.Rows = append(s.Rows, []Button{{Text: b.t(r, "btn.copy", nil), Copy: sub.SubURL}})
	}
	if sub.NextTariffID != 0 {
		if t, err := b.core.Tariff(ctx, sub.NextTariffID); err == nil {
			s.Text += "\n\n" + b.t(r, "sub.next", map[string]string{"tariff": t.Name, "date": date(sub.ExpiresAt)})
		}
		s.Rows = append(s.Rows, []Button{b.btn(r, "btn.cancel_plan", "pcan")})
	}
	s.Rows = append(s.Rows, []Button{b.btn(r, "btn.connect", "con"), b.btn(r, "btn.extend", "buy")})
	if offer, err := b.core.Addons(ctx, r.user.ID); err == nil {
		var row []Button
		if len(offer.Traffic) > 0 {
			row = append(row, b.btn(r, "btn.buy_traffic", "at"))
		}
		if offer.DevicePrice > 0 {
			row = append(row, b.btn(r, "btn.buy_device", "ad"))
		}
		if len(row) > 0 {
			s.Rows = append(s.Rows, row)
		}
	}
	if !sub.Trial {
		if sub.AutoRenew {
			s.Rows = append(s.Rows, []Button{b.btn(r, "btn.autorenew_off", "ar:0")})
		} else {
			s.Rows = append(s.Rows, []Button{b.btn(r, "btn.autorenew_on", "ar:1")})
		}
	}
	s.Rows = append(s.Rows, []Button{b.btn(r, "btn.devices", "dev"), b.btn(r, "btn.revoke", "rv")}, b.back(r, "m"))
	return b.show(ctx, r, s)
}

func (b *Bot) onAutoRenew(ctx context.Context, r *req, d Data) error {
	on := len(d.Args) == 1 && d.Args[0] == "1"
	if err := b.core.SetAutoRenew(ctx, r.user.ID, on); err != nil {
		return err
	}
	return b.showSub(ctx, r)
}

func (b *Bot) onRevokeAsk(ctx context.Context, r *req, _ Data) error {
	return b.show(ctx, r, Screen{Text: b.t(r, "revoke.ask", nil),
		Rows: [][]Button{{b.btn(r, "btn.yes", "rvok"), b.btn(r, "btn.no", "sub")}}})
}

func (b *Bot) onRevoke(ctx context.Context, r *req, _ Data) error {
	if _, err := b.core.RevokeLink(ctx, r.user.ID); err != nil {
		return err
	}
	if err := b.answer(ctx, r, b.t(r, "revoke.done", nil)); err != nil {
		return err
	}
	return b.showSub(ctx, r)
}

// showDevices показывает устройства из панели с кнопкой отвязки у каждого.
func (b *Bot) showDevices(ctx context.Context, r *req) error {
	list, err := b.core.Devices(ctx, r.user.ID)
	if err != nil {
		return err
	}
	limit := b.t(r, "sub.unlimited", nil)
	if sub, hasSub, err := b.sub(ctx, r); err == nil && hasSub && !sub.Trial {
		if t, err := b.core.Tariff(ctx, sub.TariffID); err == nil {
			limit = b.count(r, t.Devices)
		}
	}
	s := Screen{}
	var lines []string
	var dels []Button
	for i, dv := range list {
		n := strconv.Itoa(i + 1)
		lines = append(lines, b.t(r, "devices.item", map[string]string{"n": n, "name": strings.TrimSpace(dv.Platform + " " + dv.Model)}))
		dels = append(dels, Button{Text: b.t(r, "btn.dev_del", map[string]string{"n": n}), Data: data("devdel", i+1, len(list))})
	}
	s.Rows = slices.AppendSeq(s.Rows, slices.Chunk(dels, 4))
	text := b.t(r, "devices.empty", nil)
	if len(lines) > 0 {
		text = strings.Join(lines, "\n")
		s.Rows = append(s.Rows, []Button{b.btn(r, "btn.dev_reset", "devrst")})
	}
	s.Text = b.t(r, "devices.list", map[string]string{"count": strconv.Itoa(len(list)), "limit": limit, "list": text})
	s.Rows = append(s.Rows, b.back(r, "sub"))
	return b.show(ctx, r, s)
}

// onDeviceDelete отвязывает устройство по номеру. Список перечитывается: если он изменился с показа,
// бот не угадывает, а показывает свежий список.
func (b *Bot) onDeviceDelete(ctx context.Context, r *req, d Data) error {
	n, total, ok := d.pair()
	if !ok {
		return core.ErrInvalid
	}
	list, err := b.core.Devices(ctx, r.user.ID)
	if err != nil {
		return err
	}
	if int64(len(list)) != total || n > total {
		_ = b.answer(ctx, r, b.t(r, "devices.changed", nil))
		return b.showDevices(ctx, r)
	}
	if err := b.core.DeleteDevice(ctx, r.user.ID, list[n-1].HWID); err != nil {
		return err
	}
	return b.showDevices(ctx, r)
}

func (b *Bot) onDeviceReset(ctx context.Context, r *req, _ Data) error {
	if err := b.core.ResetDevices(ctx, r.user.ID); err != nil {
		return err
	}
	return b.showDevices(ctx, r)
}

// onTrial выдаёт пробный период. Не подписан на канал: кнопка канала и «Проверить».
func (b *Bot) onTrial(ctx context.Context, r *req, _ Data) error {
	sub, err := b.core.Trial(ctx, r.user.ID)
	if errors.Is(err, core.ErrNotSubscribed) {
		return b.show(ctx, r, Screen{Text: b.t(r, "trial.channel", nil), Rows: [][]Button{
			{{Text: b.t(r, "btn.channel", nil), URL: channelURL(b.cfg.String("trial.channel_url"), b.cfg.String("trial.channel"))}},
			{b.btn(r, "btn.check", "trial")},
			b.back(r, "m"),
		}})
	}
	if err != nil {
		return err
	}
	r.user.TrialUsed = true
	if err := b.say(ctx, r.chatID, b.t(r, "trial.done", map[string]string{"days": strconv.Itoa(sub.PeriodDays)})); err != nil {
		return err
	}
	return b.showConnect(ctx, r)
}

// channelURL делает ссылку на канал: ссылка из настройки, если она есть (нужна для закрытого канала
// с числовым id), иначе из @имени.
func channelURL(link, ch string) string {
	if strings.HasPrefix(link, "https://") {
		return link
	}
	return "https://t.me/" + strings.TrimPrefix(ch, "@")
}
