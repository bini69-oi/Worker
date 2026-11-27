package core

import (
	"html"
	"net/url"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// maxTagDepth это предел вложенности тегов: Telegram и без того отвергает глубокие деревья.
const maxTagDepth = 16

// allowedTags это теги Telegram HTML, которые мы пропускаем в рассылке.
var allowedTags = map[string]bool{"b": true, "i": true, "u": true, "s": true, "a": true, "code": true, "pre": true, "blockquote": true}

var (
	langClassRe = regexp.MustCompile(`^language-[a-z0-9+#_-]{1,20}$`)
	entityRe    = regexp.MustCompile(`^&(lt|gt|amp|quot|#[0-9]{1,6}|#x[0-9a-fA-F]{1,5});`)
)

func badMarkup(detail string) error { return WithDetail(ErrBadMarkup, detail) }

// CheckMarkup проверяет текст рассылки: разметка Telegram HTML с разрешёнными тегами (b, i, u, s, a, code, pre,
// blockquote), теги закрыты и вложены правильно, ссылки только https и tg, знаки <, & экранированы.
// Длина до 4096 символов. Ошибка несёт пояснение, что именно не так.
func CheckMarkup(text string) error {
	if strings.TrimSpace(text) == "" {
		return badMarkup("текст пуст")
	}
	if utf8.RuneCountInString(text) > maxMessage {
		return ErrTooLong
	}
	var stack []string
	for i := 0; i < len(text); {
		switch text[i] {
		case '&':
			if !entityRe.MatchString(text[i:]) {
				return badMarkup("знак & пишите как &amp;")
			}
			i += len(entityRe.FindString(text[i:]))
		case '<':
			end := strings.IndexByte(text[i:], '>')
			if end < 0 {
				return badMarkup("знак < пишите как &lt;")
			}
			var err error
			if stack, err = checkTag(text[i+1:i+end], stack); err != nil {
				return err
			}
			i += end + 1
		default:
			i++
		}
	}
	if len(stack) > 0 {
		return badMarkup("тег <" + stack[len(stack)-1] + "> не закрыт")
	}
	return nil
}

// checkTag разбирает один тег (без угловых скобок) и меняет стек открытых тегов.
func checkTag(body string, stack []string) ([]string, error) {
	if name, closing := strings.CutPrefix(body, "/"); closing {
		name = strings.ToLower(strings.TrimSpace(name))
		if len(stack) == 0 || !allowedTags[name] {
			return stack, badMarkup("лишний закрывающий тег </" + name + ">")
		}
		if top := stack[len(stack)-1]; top != name {
			return stack, badMarkup("тег <" + top + "> не закрыт перед </" + name + ">")
		}
		return stack[:len(stack)-1], nil
	}
	if strings.HasSuffix(body, "/") {
		return stack, badMarkup("тег <" + strings.ToLower(strings.TrimSuffix(body, "/")) + "/> нужно закрывать парным тегом")
	}
	name, attrs, _ := strings.Cut(body, " ")
	name = strings.ToLower(name)
	if !allowedTags[name] {
		return stack, badMarkup("тег <" + name + "> не разрешён")
	}
	if len(stack) >= maxTagDepth {
		return stack, badMarkup("слишком глубокая вложенность тегов")
	}
	if err := checkAttrs(name, attrs); err != nil {
		return stack, err
	}
	return append(stack, name), nil
}

var attrRe = regexp.MustCompile(`^\s*([A-Za-z][A-Za-z0-9-]*)(?:\s*=\s*(?:"([^"]*)"|'([^']*)'))?\s*`)

// checkAttrs проверяет атрибуты: у a только href на https или tg, у code только class="language-…",
// у blockquote только expandable. Остальное не разрешено.
func checkAttrs(tag, attrs string) error {
	hasHref := false
	for rest := attrs; strings.TrimSpace(rest) != ""; {
		m := attrRe.FindStringSubmatch(rest)
		if m == nil {
			return badMarkup("атрибуты тега <" + tag + "> не разобраны")
		}
		rest = rest[len(m[0]):]
		key, val := strings.ToLower(m[1]), m[2]+m[3]
		switch {
		case tag == "a" && key == "href":
			if err := CheckButtonURL(html.UnescapeString(val)); err != nil {
				return err
			}
			hasHref = true
		case tag == "code" && key == "class" && langClassRe.MatchString(val):
		case tag == "blockquote" && key == "expandable" && m[2]+m[3] == "":
		default:
			return badMarkup("атрибут " + key + " у тега <" + tag + "> не разрешён")
		}
	}
	if tag == "a" && !hasHref {
		return badMarkup("у ссылки нет атрибута href")
	}
	return nil
}

// CheckButtonURL проверяет ссылку кнопки или ссылки в тексте: только https с адресом сайта или tg.
// Пробелы по краям и управляющие знаки не допускаются: браузеры их отбрасывают и спасают javascript:.
func CheckButtonURL(raw string) error {
	bad := badMarkup("ссылка должна вести на https или tg")
	if raw == "" || strings.IndexFunc(raw, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0 {
		return bad
	}
	u, err := url.Parse(raw)
	if err != nil {
		return bad
	}
	switch strings.ToLower(u.Scheme) {
	case "https":
		if u.Host == "" {
			return bad
		}
	case "tg":
	default:
		return bad
	}
	return nil
}
