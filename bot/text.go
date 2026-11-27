package bot

import (
	"html"
	"strconv"
	"strings"
)

// rawVars это подстановки, которые уже собраны из текстов по ключу и экранированы: их не трогаем.
var rawVars = map[string]bool{"status": true, "history": true, "list": true, "user": true}

// t отдаёт текст по ключу на языке абонента. Тексты размечены HTML, поэтому каждая подстановка
// экранируется: название тарифа или модель устройства с `<` не сломает сообщение.
func (b *Bot) t(r *req, key string, vars map[string]string) string {
	safe := make(map[string]string, len(vars))
	for k, v := range vars {
		if !rawVars[k] {
			v = html.EscapeString(v)
		}
		safe[k] = v
	}
	return b.tx.Render(key, r.lang, safe)
}

// lang выбирает язык абонента по коду Telegram: ru, uk, be и kk дают русский, любой другой код
// английский, пустой код язык по умолчанию из настройки lang.default (или русский).
func (b *Bot) lang(code string) string {
	code = strings.ToLower(code)
	if i := strings.IndexByte(code, '-'); i > 0 {
		code = code[:i]
	}
	switch code {
	case "":
		if def := b.cfg.String("lang.default"); def != "" {
			return def
		}
		return "ru"
	case "ru", "uk", "be", "kk":
		return "ru"
	}
	return "en"
}

// money пишет копейки рублями: 25000 → «250 ₽», 25050 → «250,50 ₽».
func money(kop int64) string {
	sign := ""
	if kop < 0 {
		sign, kop = "-", -kop
	}
	rub := strconv.FormatInt(kop/100, 10)
	if k := kop % 100; k != 0 {
		rub += "," + strconv.FormatInt(k/10, 10) + strconv.FormatInt(k%10, 10)
	}
	return sign + rub + " ₽"
}

// gb пишет байты гигабайтами, 0 значит без лимита.
func (b *Bot) gb(r *req, bytes int64) string {
	if bytes <= 0 {
		return b.t(r, "sub.unlimited", nil)
	}
	return b.t(r, "sub.gb", map[string]string{"gb": strconv.FormatInt(bytes/(1<<30), 10)})
}

// count пишет лимит числом, 0 значит без лимита.
func (b *Bot) count(r *req, n int) string {
	if n <= 0 {
		return b.t(r, "sub.unlimited", nil)
	}
	return strconv.Itoa(n)
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

func parseInt(s string) (int64, bool) {
	n, err := strconv.ParseInt(s, 10, 64)
	return n, err == nil
}

// sysReq это запрос от имени системы: тексты на языке по умолчанию. Им пишутся сообщения админам.
func (b *Bot) sysReq() *req { return &req{lang: b.lang("")} }
