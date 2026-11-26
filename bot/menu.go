package bot

import (
	"context"
	"errors"
	"math"
	"strconv"
	"time"

	"github.com/bini69-oi/worker/core"
)

// register связывает действия кнопок с обработчиками. Короткие имена экономят 64 байта данных кнопки.
func (b *Bot) register() {
	b.acts["m"] = func(ctx context.Context, r *req, _ Data) error { return b.showMenu(ctx, r) }
	b.acts["help"] = func(ctx context.Context, r *req, _ Data) error { return b.showHelp(ctx, r) }
	b.acts["sub"] = func(ctx context.Context, r *req, _ Data) error { return b.showSub(ctx, r) }
	b.acts["ar"] = b.onAutoRenew
	b.acts["rv"] = b.onRevokeAsk
	b.acts["rvok"] = b.onRevoke
	b.acts["dev"] = func(ctx context.Context, r *req, _ Data) error { return b.showDevices(ctx, r) }
	b.acts["devdel"] = b.onDeviceDelete
	b.acts["devrst"] = b.onDeviceReset
	b.acts["trial"] = b.onTrial
	b.acts["buy"] = func(ctx context.Context, r *req, _ Data) error { return b.showTariffs(ctx, r) }
	b.acts["bt"] = b.onTariff
	b.acts["bp"] = b.onPeriod
	b.acts["bok"] = b.onBuy
	b.acts["bal"] = func(ctx context.Context, r *req, _ Data) error { return b.showBalance(ctx, r) }
	b.acts["ref"] = func(ctx context.Context, r *req, _ Data) error { return b.showRef(ctx, r) }
	b.acts["promo"] = b.onPromoAsk
	b.acts["lang"] = func(ctx context.Context, r *req, _ Data) error { return b.showLangs(ctx, r) }
	b.acts["ln"] = b.onLang
	b.acts["pchk"] = b.onCheckPay
	b.acts["con"] = func(ctx context.Context, r *req, _ Data) error { return b.showConnect(ctx, r) }
	b.inputs[waitPromo] = b.onPromoInput
}

// showMenu показывает главное меню.
func (b *Bot) showMenu(ctx context.Context, r *req) error {
	s, err := b.menu(ctx, r)
	if err != nil {
		return err
	}
	return b.show(ctx, r, s)
}

func (b *Bot) menu(ctx context.Context, r *req) (Screen, error) {
	sub, hasSub, err := b.sub(ctx, r)
	if err != nil {
		return Screen{}, err
	}
	s := Screen{Text: b.t(r, "bot.menu", map[string]string{"status": b.status(r, sub, hasSub), "balance": money(r.user.Balance)})}
	if b.trialOpen(r, hasSub) {
		s.Rows = append(s.Rows, []Button{b.btn(r, "btn.trial", "trial")})
	}
	s.Rows = append(s.Rows,
		[]Button{b.btn(r, "btn.sub", "sub"), b.btn(r, "btn.connect", "con")},
		[]Button{b.btn(r, "btn.buy", "buy"), b.btn(r, "btn.balance", "bal")},
		[]Button{b.btn(r, "btn.ref", "ref"), b.btn(r, "btn.promo", "promo")},
		[]Button{b.btn(r, "btn.support", "help")},
	)
	if len(b.tx.Langs()) > 1 {
		s.Rows = append(s.Rows, []Button{b.btn(r, "btn.lang", "lang")})
	}
	return s, nil
}

// sub берёт подписку абонента. hasSub=false, если её никогда не было.
func (b *Bot) sub(ctx context.Context, r *req) (core.Subscription, bool, error) {
	s, err := b.core.Sub(ctx, r.user.ID)
	if errors.Is(err, core.ErrNotFound) {
		return s, false, nil
	}
	return s, err == nil, err
}

// trialOpen говорит, показывать ли кнопку пробного периода: включён, не брали и подписки не было.
func (b *Bot) trialOpen(r *req, hasSub bool) bool {
	return b.cfg.Bool("trial.enabled") && b.cfg.Int("trial.days") > 0 && !r.user.TrialUsed && !hasSub
}

// status пишет строку о подписке: активна, пробная, закончилась или нет.
func (b *Bot) status(r *req, s core.Subscription, hasSub bool) string {
	now := b.now()
	switch {
	case !hasSub:
		return b.t(r, "status.none", nil)
	case s.Status != core.SubActive || !s.ExpiresAt.After(now):
		return b.t(r, "status.expired", map[string]string{"date": date(s.ExpiresAt)})
	case s.Trial:
		return b.t(r, "status.trial", map[string]string{"date": date(s.ExpiresAt), "days": daysLeft(s.ExpiresAt, now)})
	}
	return b.t(r, "status.active", map[string]string{"date": date(s.ExpiresAt), "days": daysLeft(s.ExpiresAt, now)})
}

// btn это кнопка с текстом по ключу и данными действия.
func (b *Bot) btn(r *req, key, data string) Button {
	return Button{Text: b.t(r, key, nil), Data: data}
}

func (b *Bot) back(r *req, data string) []Button { return []Button{b.btn(r, "btn.back", data)} }

func date(t time.Time) string { return t.Format("02.01.2006") }

// daysLeft считает оставшиеся дни с округлением вверх: 1 час до конца это «1 дн.», а не «0».
func daysLeft(until, now time.Time) string {
	d := until.Sub(now).Hours() / 24
	return strconv.Itoa(int(math.Max(0, math.Ceil(d))))
}

// showHelp показывает команды и как написать в поддержку.
func (b *Bot) showHelp(ctx context.Context, r *req) error {
	return b.show(ctx, r, Screen{Text: b.t(r, "help.text", nil), Rows: [][]Button{b.back(r, "m")}})
}
