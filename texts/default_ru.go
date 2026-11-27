package texts

import "slices"

// Def описывает один текст: ключ, допустимые подстановки и русский и английский тексты по умолчанию.
// Строк для абонента вне этого списка нет. Новые ключи добавляют сюда.
type Def struct {
	Key     string
	Params  []string // допустимые подстановки без фигурных скобок
	Default string   // русский
	En      string   // английский
}

// codeLang это язык, на котором написаны умолчания в коде.
const codeLang = "ru"

// enLang это второй язык умолчаний.
const enLang = "en"

var coreDefs = []Def{
	{Key: "buy.success", Params: []string{"days"},
		Default: "Подписка оплачена. Она действует ещё {days} дн.",
		En:      "Subscription paid. It runs for {days} more days."},
	{Key: "sub.expiring", Params: []string{"days"},
		Default: "Осталось {days} дн.",
		En:      "{days} days left"},
}

// defs это все тексты: по файлу на часть, чтобы ни один файл не разрастался.
var defs = slices.Concat(coreDefs, errDefs, botDefs, notifyDefs, admDefs)

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
