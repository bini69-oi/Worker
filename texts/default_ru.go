package texts

import "slices"

// Def описывает один текст: ключ, допустимые подстановки и русский текст по умолчанию.
// Строк для абонента вне этого списка нет. Новые ключи добавляют сюда.
type Def struct {
	Key     string
	Params  []string // допустимые подстановки без фигурных скобок
	Default string
}

// codeLang это язык, на котором написаны умолчания в коде.
const codeLang = "ru"

var coreDefs = []Def{
	{Key: "buy.success", Params: []string{"days"},
		Default: "Подписка оплачена. Она действует ещё {days} дн."},
	{Key: "sub.expiring", Params: []string{"days"},
		Default: "Осталось {days} дн."},
}

// defs это все тексты: по файлу на часть, чтобы ни один файл не разрастался.
var defs = slices.Concat(coreDefs, errDefs, botDefs)

// Defs возвращает копию списка текстов.
func Defs() []Def {
	return append([]Def(nil), defs...)
}

func find(key string) (Def, bool) {
	for _, d := range defs {
		if d.Key == key {
			return d, true
		}
	}
	return Def{}, false
}
