package core

import "strings"

// Lang приводит код языка Telegram к языку текстов: русский, украинский, белорусский и казахский дают ru,
// пустой код язык по умолчанию из настроек, остальные en. Бот и кабинет берут язык нового абонента отсюда.
func (c *Core) Lang(code string) string {
	code = strings.ToLower(code)
	if i := strings.IndexByte(code, '-'); i > 0 {
		code = code[:i]
	}
	switch code {
	case "":
		if def := c.cfg.String("lang.default"); def != "" {
			return def
		}
		return "ru"
	case "ru", "uk", "be", "kk":
		return "ru"
	}
	return "en"
}
