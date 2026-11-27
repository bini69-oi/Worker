package core

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/bini69-oi/worker/settings"
)

// ConfigAdmin это настройки глазами админки: список, проверка, запись и сброс. Реализует его пакет settings.
type ConfigAdmin interface {
	Validate(key, raw string) error
	Set(key, raw string) error
	Reset(key string) error
	Current(key string) (value string, changed bool)
	Masked(key string) settings.Masked
}

// SettingResult это итог правки настройки. Pending значит, что настройка секретная или денежная: она применится
// только после «Да» владельца в Telegram, до тех пор действует старое значение.
type SettingResult struct {
	Pending   bool
	ConfirmID int64
}

// settingPayload это то, что владелец подтверждает кнопкой «Да»: настройка и новое значение (или сброс).
type settingPayload struct {
	Key   string `json:"key"`
	Value string `json:"value,omitempty"`
	Reset bool   `json:"reset,omitempty"`
}

func (c *Core) configAdmin() (ConfigAdmin, error) {
	cfg, ok := c.cfg.(ConfigAdmin)
	if !ok {
		return nil, ErrInternal
	}
	return cfg, nil
}

// settingErr делает из ошибки настроек ответ админу: что не так, без самого значения.
func settingErr(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, settings.ErrUnknownKey):
		return ErrNotFound
	case errors.Is(err, settings.ErrBadValue):
		return WithDetail(ErrBadSetting, err.Error())
	}
	return err
}

// SetSetting меняет настройку. Только владелец. Значение проверяется по типу и своей проверке настройки.
// Обычная настройка применяется сразу и пишется в журнал. Секретная идёт через RequestSecret, денежная и
// admin.allow_ips через RequestAction: применяются после «Да» владельца, до того действует старое значение.
func (c *Core) SetSetting(ctx context.Context, actorTg int64, key, value string) (SettingResult, error) {
	return c.changeSetting(ctx, actorTg, settingPayload{Key: key, Value: value})
}

// ResetSetting возвращает настройку к умолчанию. Только владелец. Сброс секретной и денежной настройки тоже
// ждёт «Да».
func (c *Core) ResetSetting(ctx context.Context, actorTg int64, key string) (SettingResult, error) {
	return c.changeSetting(ctx, actorTg, settingPayload{Key: key, Reset: true})
}

func (c *Core) changeSetting(ctx context.Context, actorTg int64, p settingPayload) (SettingResult, error) {
	var res SettingResult
	cfg, err := c.configAdmin()
	if err != nil {
		return res, err
	}
	if _, err := c.Require(ctx, actorTg, PermOwner, "setting.set"); err != nil {
		return res, err
	}
	def, ok := settingDef(p.Key)
	if !ok {
		return res, ErrNotFound
	}
	if !p.Reset {
		if err := cfg.Validate(p.Key, p.Value); err != nil {
			return res, settingErr(err)
		}
	}
	if err := c.checkGatewayKeys(p); err != nil {
		return res, err
	}
	// Тему «Безопасность» выключить нельзя ни здесь, ни в чужой проверке: о входах и смене ключей владелец знает всегда.
	if p.Key == "notify.security" && !p.Reset && p.Value != "true" {
		return res, WithDetail(ErrBadSetting, "тему безопасности выключить нельзя")
	}
	switch {
	case def.Secret && !p.Reset:
		res.ConfirmID, err = c.RequestSecret(ctx, actorTg, p.Key, p.Value)
		res.Pending = true
	case settings.NeedsConfirm(p.Key):
		res.ConfirmID, err = c.RequestAction(ctx, actorTg, "setting", p)
		res.Pending = true
	default:
		err = c.ownerAction(ctx, actorTg, "setting.set", func() (string, map[string]string, error) { return c.applySetting(cfg, p) })
	}
	return res, err
}

// settingDef находит настройку в реестре.
func settingDef(key string) (settings.Def, bool) {
	for _, d := range settings.Registry() {
		if d.Key == key {
			return d, true
		}
	}
	return settings.Def{}, false
}

// applySetting записывает настройку и отдаёт, что писать в журнал: прежнее и новое значение. Значение секрета
// в журнал не попадает.
func (c *Core) applySetting(cfg ConfigAdmin, p settingPayload) (string, map[string]string, error) {
	def, _ := settingDef(p.Key)
	old, _ := cfg.Current(p.Key)
	var err error
	if p.Reset {
		err = cfg.Reset(p.Key)
	} else if def.Secret {
		return "", nil, ErrInvalid // секрет меняет только RequestSecret
	} else {
		err = cfg.Set(p.Key, p.Value)
	}
	if err != nil {
		return "", nil, settingErr(err)
	}
	cur, _ := cfg.Current(p.Key)
	details := map[string]string{"old": old, "new": cur}
	if def.Secret {
		details = map[string]string{"reset": "true"}
	}
	return p.Key, details, nil
}

// onSettingApproved выполняет настройку, которую владелец подтвердил кнопкой «Да».
func (c *Core) onSettingApproved(ctx context.Context, payload []byte) error {
	var p settingPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		return err
	}
	cfg, err := c.configAdmin()
	if err != nil {
		return err
	}
	return c.ownerAction(ctx, c.owner, "setting.set", func() (string, map[string]string, error) { return c.applySetting(cfg, p) })
}

// SettingInfo это настройка для экрана: описание, умолчание, текущее значение (у секрета маска).
type SettingInfo struct {
	Key     string
	Kind    string
	Group   string
	Desc    string
	Default string
	Value   string // у секрета маска: ••••1a2b
	Changed bool
	Secret  bool
	Confirm bool // меняется только через «Да» владельца
}

// maskText показывает секрет маской: четыре точки и последние 4 символа длинного секрета.
func maskText(m settings.Masked) string {
	if !m.IsSet {
		return ""
	}
	return "••••" + m.Last4
}

// SettingList отдаёт все настройки реестра для экрана. Только владелец. Секреты только маской.
func (c *Core) SettingList(ctx context.Context, actorTg int64) ([]SettingInfo, error) {
	cfg, err := c.configAdmin()
	if err != nil {
		return nil, err
	}
	if _, err := c.Require(ctx, actorTg, PermOwner, "setting.list"); err != nil {
		return nil, err
	}
	var out []SettingInfo
	for _, d := range settings.Registry() {
		v, changed := cfg.Current(d.Key)
		if d.Secret {
			v = maskText(cfg.Masked(d.Key))
		}
		out = append(out, SettingInfo{Key: d.Key, Kind: d.Kind.String(), Group: d.Group, Desc: d.Desc, Default: d.Default,
			Value: v, Changed: changed, Secret: d.Secret, Confirm: settings.NeedsConfirm(d.Key)})
	}
	return out, nil
}

// AllowAllAdminIPs сбрасывает список адресов админки (admin.allow_ips). Его зовёт только команда на сервере
// `worker admin allow-all`: владелец, запершийся своим списком, не может войти в админку, а из админки
// сброс идёт через «Да». Запись о сбросе идёт в журнал действий от имени владельца.
func (c *Core) AllowAllAdminIPs(ctx context.Context) error {
	cfg, err := c.configAdmin()
	if err != nil {
		return err
	}
	return c.ownerAction(ctx, c.owner, "admin.allow_all", func() (string, map[string]string, error) {
		old, _ := cfg.Current("admin.allow_ips")
		if err := cfg.Reset("admin.allow_ips"); err != nil {
			return "", nil, err
		}
		return "admin.allow_ips", map[string]string{"old": old}, nil
	})
}
