package texts

// appDefs это тексты кабинета (ключи app.*) и тексты бота, которые нужны входу на сайт.
var appDefs = []Def{
	{Key: "bot.web_login", Params: []string{"ip", "agent"},
		Default: "🔐 <b>Вход на сайт</b>\nIP: {ip}\nБраузер: {agent}\n\nНажмите код, который видите на сайте. Нужен он только если вы сами входите на сайт прямо сейчас. Это не вы? Нажмите «Это не я».",
		En:      "🔐 <b>Website login</b>\nIP: {ip}\nBrowser: {agent}\n\nTap the code you see on the site. You only need it if you are logging in to the site right now. Not you? Tap “Not me”."},
	{Key: "bot.web_login_ok", Default: "✅ Вход подтверждён. Вернитесь на сайт.",
		En: "✅ Login confirmed. Go back to the site."},
	{Key: "bot.web_login_no", Default: "🚫 Вход отклонён. Если это были не вы, ничего делать не нужно.",
		En: "🚫 Login rejected. If it wasn't you, there's nothing to do."},
	{Key: "app.stars_title", Default: "Пополнение баланса", En: "Top up balance"},
	{Key: "app.stars_desc", Default: "Звёзды Telegram зачислятся на баланс в рублях по курсу", En: "Telegram Stars will be credited to your balance in rubles at the current rate"},
}
