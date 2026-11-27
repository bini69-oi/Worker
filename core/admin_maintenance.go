package core

import (
	"context"
	"strconv"
	"strings"
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
	return c.ownerAction(ctx, actorTg, action, func() (string, map[string]string, error) {
		if text != "" && c.texts != nil {
			for _, key := range []string{"bot.maintenance", "err.maintenance"} {
				if err := c.texts.Set(key, "ru", text); err != nil {
					return "", nil, textErr(err)
				}
			}
		}
		if err := cfg.Set("maintenance.on", strconv.FormatBool(on)); err != nil {
			return "", nil, settingErr(err)
		}
		return "maintenance.on", map[string]string{"on": strconv.FormatBool(on), "text_len": itoa(int64(utf8.RuneCountInString(text)))}, nil
	})
}
