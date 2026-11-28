package core

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/bini69-oi/worker/texts"
)

// TextEditor это правка текстов бота. Реализует её пакет texts.
type TextEditor interface {
	// Check проверяет правку, не записывая. Запись идёт транзакцией ядра, потом Apply или ApplyReset.
	Check(key, lang, value string) error
	CheckKey(key string) error
	Apply(key, lang, value string)
	ApplyReset(key, lang string)
	// Override отдаёт правку админа. Правки нет: false, действует умолчание из кода.
	Override(key, lang string) (string, bool)
	Langs() []string
	// Raw отдаёт текст как его видит абонент: правка или умолчание.
	Raw(key, lang string) string
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
	return c.ownerAction(ctx, actorTg, "text.set", func(tx Tx, now time.Time, res *ownerResult) error {
		if sentAsHTML(key) && strings.TrimSpace(value) != "" {
			if err := CheckMarkup(value); err != nil {
				return err
			}
		}
		if err := c.texts.Check(key, lang, value); err != nil {
			return textErr(err)
		}
		if err := tx.SaveText(key, lang, value, now); err != nil {
			return err
		}
		res.Target, res.Details = key, map[string]string{"lang": lang, "len": itoa(int64(utf8.RuneCountInString(value)))}
		res.After = append(res.After, func() { c.texts.Apply(key, lang, value) })
		return nil
	})
}

// ResetText возвращает текст к умолчанию из кода. Только владелец.
func (c *Core) ResetText(ctx context.Context, actorTg int64, key, lang string) error {
	if c.texts == nil {
		return ErrInternal
	}
	return c.ownerAction(ctx, actorTg, "text.reset", func(tx Tx, _ time.Time, res *ownerResult) error {
		if err := c.texts.CheckKey(key); err != nil {
			return textErr(err)
		}
		if err := tx.DeleteText(key, lang); err != nil {
			return err
		}
		res.Target, res.Details = key, map[string]string{"lang": lang}
		res.After = append(res.After, func() { c.texts.ApplyReset(key, lang) })
		return nil
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

// sentAsHTML говорит, что текст бот шлёт с разметкой Telegram HTML: тогда он проходит ту же проверку разметки, что
// и рассылка. Тексты кабинета (app.*) идут в JSON, подписи кнопок (btn.*) это простой текст.
func sentAsHTML(key string) bool {
	return !strings.HasPrefix(key, "app.") && !strings.HasPrefix(key, "btn.")
}
