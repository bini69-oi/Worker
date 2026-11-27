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

	// Общие состояния экрана и форматы.
	{Key: "app.loading", Default: "Загрузка", En: "Loading"},
	{Key: "app.offline", Default: "Нет связи", En: "No connection"},
	{Key: "app.offline_hint", Default: "Проверьте интернет и повторите", En: "Check your internet and try again"},
	{Key: "app.retry", Default: "Повторить", En: "Retry"},
	{Key: "app.empty", Default: "Здесь пока пусто", En: "Nothing here yet"},
	{Key: "app.error_code", Params: []string{"code"}, Default: "Код ошибки: {code}", En: "Error code: {code}"},
	{Key: "app.money", Params: []string{"amount"}, Default: "{amount} ₽", En: "{amount} ₽"},
	{Key: "app.unlimited", Default: "без лимита", En: "unlimited"},
	{Key: "app.gb", Params: []string{"n"}, Default: "{n} ГБ", En: "{n} GB"},

	// Вход в браузере.
	{Key: "app.login_title", Default: "Вход в кабинет", En: "Sign in"},
	{Key: "app.login_step1", Default: "Откройте бота кнопкой ниже.", En: "Open the bot with the button below."},
	{Key: "app.login_step2", Default: "Нажмите в боте тот же код, что виден здесь.", En: "Tap the same code in the bot as shown here."},
	{Key: "app.login_code_label", Default: "Код на этом экране", En: "Code on this screen"},
	{Key: "app.login_button", Default: "Войти через Telegram", En: "Sign in with Telegram"},
	{Key: "app.login_waiting", Default: "Ждём подтверждения в Telegram", En: "Waiting for confirmation in Telegram"},
	{Key: "app.login_rejected", Default: "Вход отклонён, начните заново", En: "Sign-in rejected, please start over"},
	{Key: "app.login_expired", Default: "Время вышло, начните заново", En: "Time ran out, please start over"},
	{Key: "app.login_restart", Default: "Начать заново", En: "Start over"},

	// Главная.
	{Key: "app.back", Default: "Назад", En: "Back"},
	{Key: "app.home_balance", Default: "Баланс", En: "Balance"},
	{Key: "app.home_status_active", Default: "Активна", En: "Active"},
	{Key: "app.home_status_trial", Default: "Пробная", En: "Trial"},
	{Key: "app.home_status_expired", Default: "Истекла", En: "Expired"},
	{Key: "app.home_status_none", Default: "Нет подписки", En: "No subscription"},
	{Key: "app.home_none_hint", Default: "Подписки пока нет. Попробуйте бесплатно или выберите тариф.", En: "No subscription yet. Try it for free or pick a plan."},
	{Key: "app.home_until", Params: []string{"date"}, Default: "до {date}", En: "until {date}"},
	{Key: "app.days_unit", Default: "дн.", En: "days"},
	{Key: "app.home_connect", Default: "Подключить", En: "Connect"},
	{Key: "app.home_trial", Default: "Попробовать бесплатно", En: "Try for free"},
	{Key: "app.home_buy", Default: "Купить", En: "Buy"},
	{Key: "app.home_renew", Default: "Продлить", En: "Renew"},
	{Key: "app.tile_sub", Default: "Подписка", En: "Subscription"},
	{Key: "app.tile_devices", Default: "Устройства", En: "Devices"},
	{Key: "app.tile_ref", Default: "Пригласить друзей", En: "Invite friends"},
	{Key: "app.tile_promo", Default: "Промокод", En: "Promo code"},
	{Key: "app.tile_history", Default: "История", En: "History"},
	{Key: "app.tile_support", Default: "Поддержка", En: "Support"},
}
