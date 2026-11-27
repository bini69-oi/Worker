package core

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/bini69-oi/worker/settings"
	"github.com/bini69-oi/worker/texts"
)

// TextEditor это правка текстов бота. Реализует её пакет texts.
type TextEditor interface {
	Set(key, lang, value string) error
	Reset(key, lang string) error
	// Override отдаёт правку админа. Правки нет: false, действует умолчание из кода.
	Override(key, lang string) (string, bool)
	Langs() []string
}

// ownerAction выполняет действие владельца над настройками бота. Порядок такой: право (отказ в журнал и тревогой),
// само действие, запись в журнал. Настройки и тексты лежат в своих хранилищах с их записью, а транзакция ядра
// занимает единственное соединение записи: делать то и другое внутри неё нельзя, поэтому журнал пишется сразу после
// успешного действия. Не получилось действие: журнала нет, ошибка идёт наверх.
func (c *Core) ownerAction(ctx context.Context, actorTg int64, action string,
	do func() (target string, details map[string]string, err error)) error {
	if _, err := c.Require(ctx, actorTg, PermOwner, action); err != nil {
		return err
	}
	target, details, err := do()
	if err != nil {
		return err
	}
	return c.asAdmin(ctx, actorTg, PermOwner, action, func(Tx, Admin, time.Time) (string, map[string]string, error) {
		return target, details, nil
	})
}

// maxButton это предел подписи кнопки Telegram.
const maxButton = 64

// textLimit это предел длины текста по ключу: подписи кнопок (btn.*) до 64 знаков, остальное до 4096.
func textLimit(key string) int {
	if strings.HasPrefix(key, "btn.") {
		return maxButton
	}
	return maxMessage
}

// textErr переводит ошибку пакета texts в ошибку ядра. Пояснение для владельца несёт список допустимых подстановок.
func textErr(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, texts.ErrUnknownKey):
		return ErrNotFound
	case errors.Is(err, texts.ErrBadLang):
		return ErrInvalid
	case errors.Is(err, texts.ErrUnknownParam), errors.Is(err, texts.ErrEmptyText):
		return WithDetail(ErrBadText, err.Error())
	}
	return err
}

// SetText меняет текст бота для языка. Только владелец. Неизвестная подстановка, подпись кнопки длиннее 64 знаков
// и сообщение длиннее 4096 не сохраняются.
func (c *Core) SetText(ctx context.Context, actorTg int64, key, lang, value string) error {
	if c.texts == nil {
		return ErrInternal
	}
	if utf8.RuneCountInString(value) > textLimit(key) {
		if _, err := c.Require(ctx, actorTg, PermOwner, "text.set"); err != nil {
			return err
		}
		return ErrTooLong
	}
	return c.ownerAction(ctx, actorTg, "text.set", func() (string, map[string]string, error) {
		if err := c.texts.Set(key, lang, value); err != nil {
			return "", nil, textErr(err)
		}
		return key, map[string]string{"lang": lang, "len": itoa(int64(utf8.RuneCountInString(value)))}, nil
	})
}

// ResetText возвращает текст к умолчанию из кода. Только владелец.
func (c *Core) ResetText(ctx context.Context, actorTg int64, key, lang string) error {
	if c.texts == nil {
		return ErrInternal
	}
	return c.ownerAction(ctx, actorTg, "text.reset", func() (string, map[string]string, error) {
		if err := c.texts.Reset(key, lang); err != nil {
			return "", nil, textErr(err)
		}
		return key, map[string]string{"lang": lang}, nil
	})
}

// TextInfo это текст для редактора: ключ, подстановки, умолчание и текущее значение на каждом языке.
type TextInfo struct {
	Key     string
	Params  []string
	Default map[string]string // ru, en: умолчание из кода
	Value   map[string]string // ru, en: что видит абонент сейчас
	Changed map[string]bool   // есть правка владельца
}

// TextList отдаёт все тексты для редактора. Только владелец.
func (c *Core) TextList(ctx context.Context, actorTg int64) ([]TextInfo, error) {
	if c.texts == nil {
		return nil, ErrInternal
	}
	if _, err := c.Require(ctx, actorTg, PermOwner, "text.list"); err != nil {
		return nil, err
	}
	var out []TextInfo
	for _, d := range texts.Defs() {
		info := TextInfo{Key: d.Key, Params: append([]string{}, d.Params...),
			Default: map[string]string{"ru": d.Default, "en": d.En}, Value: map[string]string{}, Changed: map[string]bool{}}
		for _, lang := range []string{"ru", "en"} {
			v, changed := c.texts.Override(d.Key, lang)
			if !changed {
				v = info.Default[lang]
			}
			info.Value[lang], info.Changed[lang] = v, changed
		}
		out = append(out, info)
	}
	return out, nil
}

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

// GatewayState это касса для экрана: включена ли, заполнены ли ключи, порядок, последняя оплата и ждущие платежи.
type GatewayState struct {
	Name       string
	Title      string
	Enabled    bool
	Configured bool // ключи заполнены
	Sort       int64
	LastPaidAt time.Time // нулевое время, если оплат ещё не было
	Pending    int
}

// GatewayStates отдаёт состояние всех касс. Только владелец.
func (c *Core) GatewayStates(ctx context.Context, actorTg int64) ([]GatewayState, error) {
	if _, err := c.Require(ctx, actorTg, PermOwner, "gateway.list"); err != nil {
		return nil, err
	}
	stats, err := c.store.GatewayStats(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]GatewayState, 0, len(c.gateways))
	for _, g := range c.gateways {
		n := g.Name()
		st := stats[n]
		out = append(out, GatewayState{Name: n, Title: c.cfg.String(n + ".title"), Enabled: c.cfg.Bool(n + ".enabled"),
			Configured: g.Configured(), Sort: c.cfg.Int(n + ".sort"), LastPaidAt: st.LastPaidAt, Pending: st.Pending})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Sort < out[j].Sort })
	return out, nil
}

// checkGatewayKeys не даёт включить кассу без ключей: абонент увидел бы кассу, которая не работает.
func (c *Core) checkGatewayKeys(p settingPayload) error {
	name, ok := strings.CutSuffix(p.Key, ".enabled")
	if !ok || p.Reset || p.Value != "true" {
		return nil
	}
	if g, found := c.gateway(name); found && !g.Configured() {
		return ErrNoKeys
	}
	return nil
}
