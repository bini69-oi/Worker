package texts

import "strings"

// ByPrefix отдаёт тексты, ключи которых начинаются с одного из prefixes, на языке lang, без подстановок:
// подставляет их тот, кто показывает текст. Кабинет берёт так все свои строки и тексты ошибок за один запрос.
func (t *Texts) ByPrefix(lang string, prefixes ...string) map[string]string {
	out := map[string]string{}
	for _, d := range defs {
		for _, p := range prefixes {
			if strings.HasPrefix(d.Key, p) {
				out[d.Key] = t.Raw(d.Key, lang)
				break
			}
		}
	}
	return out
}
