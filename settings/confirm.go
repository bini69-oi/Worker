package settings

import "strings"

// IsMoney говорит, что настройка денежная: реквизиты ручной оплаты, курс Stars, проценты и бонусы рефералки,
// цены докупки, адрес возврата из кассы. Её меняют только через «Да» владельца в Telegram: подмена реквизитов
// или процента угнанной сессией стоит денег.
func IsMoney(key string) bool {
	switch key {
	case "manual.details", "stars.rate", "ref.first_percent", "ref.first_fixed", "ref.each_percent", "ref.welcome_bonus",
		"addons.traffic", "addons.device_month":
		return true
	}
	if d, ok := find(key); ok && d.Group == "money" {
		return true
	}
	return strings.HasSuffix(key, ".return_url")
}

// NeedsConfirm говорит, что настройка меняется только через «Да» владельца: секрет, денежная или та, что
// закрывает или открывает вход в админку.
func NeedsConfirm(key string) bool {
	if d, ok := find(key); ok && d.Secret {
		return true
	}
	return IsMoney(key) || key == "admin.allow_ips"
}
