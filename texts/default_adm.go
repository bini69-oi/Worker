package texts

// admDefs это тексты админки для абонента и ошибки админки. Сама админка пишет свои подписи в коде фронта.
var admDefs = []Def{
	{Key: "err.bad_markup", Default: "Разметка не прошла проверку",
		En: "The markup did not pass the check"},
	{Key: "err.bad_text", Default: "Текст не подходит",
		En: "The text is not valid"},
	{Key: "err.bad_setting", Default: "Значение настройки не подходит",
		En: "The setting value is not valid"},
	{Key: "err.period_too_long", Default: "Период больше 90 дней",
		En: "The period is longer than 90 days"},
	{Key: "err.broadcast_busy", Default: "Дождитесь конца текущей рассылки",
		En: "Wait for the current broadcast to finish"},
	{Key: "err.unreachable", Default: "Абонент заблокировал бота, сообщение не уйдёт",
		En: "The subscriber blocked the bot, the message will not be delivered"},
	{Key: "err.already_paid", Default: "Платёж уже зачислен",
		En: "The payment is already credited"},
	{Key: "err.no_keys", Default: "Сначала заполните ключи",
		En: "Fill in the keys first"},
	{Key: "notify.support_reply", Params: []string{"text"},
		Default: "{text}",
		En:      "{text}"},
}
