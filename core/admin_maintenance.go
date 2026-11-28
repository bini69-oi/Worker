package core

import (
	"context"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// Maintenance это состояние техработ для экрана: идут ли они и какой текст видят абоненты.
type Maintenance struct {
	On   bool
	Text string
}

// MaintenanceState отдаёт состояние техработ. Только владелец.
func (c *Core) MaintenanceState(ctx context.Context, actorTg int64) (Maintenance, error) {
	if _, err := c.Require(ctx, actorTg, PermOwner, "maintenance.get"); err != nil {
		return Maintenance{}, err
	}
	m := Maintenance{On: c.cfg.Bool("maintenance.on")}
	if c.texts != nil {
		m.Text = c.texts.Raw("bot.maintenance", "ru")
	}
	return m, nil
}

// SetMaintenance включает или выключает техработы одной кнопкой. Только владелец. Непустой текст становится
// текстом о работах в боте и кабинете. Включение и выключение пишутся в журнал.
func (c *Core) SetMaintenance(ctx context.Context, actorTg int64, on bool, text string) error {
	cfg, err := c.configAdmin()
	if err != nil {
		return err
	}
	text = strings.TrimSpace(text)
	action := "maintenance.off"
	if on {
		action = "maintenance.on"
	}
	if utf8.RuneCountInString(text) > maxMessage {
		if _, err := c.Require(ctx, actorTg, PermOwner, action); err != nil {
			return err
		}
		return ErrTooLong
	}
	return c.ownerAction(ctx, actorTg, action, func(tx Tx, now time.Time, res *ownerResult) error {
		if text != "" {
			if err := CheckMarkup(text); err != nil {
				return err
			}
		}
		if text != "" && c.texts != nil {
			for _, key := range []string{"bot.maintenance", "err.maintenance"} {
				if err := c.texts.Check(key, "ru", text); err != nil {
					return textErr(err)
				}
				if err := tx.SaveText(key, "ru", text, now); err != nil {
					return err
				}
				res.After = append(res.After, func() { c.texts.Apply(key, "ru", text) })
			}
		}
		ch, err := cfg.PrepareSet("maintenance.on", strconv.FormatBool(on))
		if err != nil {
			return settingErr(err)
		}
		res.Target = "maintenance.on"
		res.Details = map[string]string{"on": strconv.FormatBool(on), "text_len": itoa(int64(utf8.RuneCountInString(text)))}
		return c.writeChange(tx, cfg, ch, now, res)
	})
}
