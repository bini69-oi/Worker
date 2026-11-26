package bot

import (
	"slices"
	"strings"
)

// t отдаёт текст по ключу на языке абонента.
func (b *Bot) t(r *req, key string, vars map[string]string) string {
	return b.tx.Render(key, r.lang, vars)
}

// lang выбирает язык абонента: язык Telegram, если на нём есть тексты, иначе язык по умолчанию.
func (b *Bot) lang(code string) string {
	code = strings.ToLower(code)
	if i := strings.IndexByte(code, '-'); i > 0 {
		code = code[:i]
	}
	if slices.Contains(b.tx.Langs(), code) {
		return code
	}
	if def := b.cfg.String("lang.default"); def != "" {
		return def
	}
	return "ru"
}
