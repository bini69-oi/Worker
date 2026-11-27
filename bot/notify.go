package bot

import (
	"context"
	"errors"
	"html"

	"github.com/bini69-oi/worker/core"
	"github.com/bini69-oi/worker/settings"
	"github.com/bini69-oi/worker/tg"
)

func init() {
	// Тему «Безопасность» выключить нельзя: о входах и смене ключей владелец узнаёт всегда.
	settings.SetCheck("notify.security", func(v string) error {
		if v != "true" {
			return errors.New("тему безопасности выключить нельзя")
		}
		return nil
	})
}

var _ core.Notifier = (*Bot)(nil)

// moneyVars это подстановки, в которых ядро передаёт копейки: бот пишет их рублями.
var moneyVars = map[string]bool{"amount": true, "missing": true, "revenue": true}

// userButton это кнопка под уведомлением абоненту: куда ведёт и какой текст.
var userButton = map[string][2]string{
	"notify.remind":           {"btn.extend", "buy"},
	"notify.expired":          {"btn.extend", "buy"},
	"notify.recurring_failed": {"btn.extend", "buy"},
	"notify.autorenew_off":    {"btn.extend", "buy"},
	"notify.bought":           {"btn.connect", "con"},
	"notify.topup":            {"btn.balance", "bal"},
	"notify.refunded":         {"btn.balance", "bal"},
	"notify.pay_review":       {"btn.balance", "bal"},
	"notify.autorenew_ok":     {"btn.sub", "sub"},
	"notify.traffic_limited":  {"btn.sub", "sub"},
	"notify.autorenew_fail":   {"btn.topup", "tp"},
	"notify.pay_expired":      {"btn.topup", "tp"},
	"notify.ref_bonus":        {"btn.ref", "ref"},
}

// User шлёт уведомление абоненту новым сообщением, экран не трогает. Абонент заблокировал бота:
// отметка «недоступен», задача считается выполненной, чтобы очередь не повторяла её впустую.
func (b *Bot) User(ctx context.Context, n core.NotifyTask) error {
	u, err := b.core.User(ctx, n.UserID)
	if err != nil {
		return err
	}
	r := &req{user: u, lang: u.Lang}
	s := Screen{Text: b.t(r, n.Key, moneyFmt(n.Vars))}
	if btn, ok := userButton[n.Key]; ok {
		s.Rows = [][]Button{{b.btn(r, btn[0], btn[1])}}
	}
	_, err = b.api.SendMessage(ctx, tg.SendParams{ChatID: u.TgID, Text: s.Text, ParseMode: "HTML", Markup: s.markup(), DisablePreview: true})
	var te *tg.Error
	if errors.As(err, &te) && te.IsBlocked() {
		return b.core.MarkUnreachable(ctx, u.ID)
	}
	return err
}

// moneyFmt переписывает копейки в рубли в подстановках.
func moneyFmt(vars map[string]string) map[string]string {
	out := make(map[string]string, len(vars))
	for k, v := range vars {
		if moneyVars[k] {
			if kop, ok := parseInt(v); ok {
				v = money(kop)
			}
		}
		out[k] = v
	}
	return out
}

// mention это ссылка на абонента: по ней админ открывает его профиль в Telegram.
func mention(u core.User) string {
	name := u.Username
	if name == "" {
		name = "id" + itoa(u.TgID)
	} else {
		name = "@" + name
	}
	return `<a href="tg://user?id=` + itoa(u.TgID) + `">` + html.EscapeString(name) + `</a>`
}
