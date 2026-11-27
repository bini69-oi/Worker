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
