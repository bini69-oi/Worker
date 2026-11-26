package texts

// botDefs это тексты и кнопки текстового бота.
var botDefs = []Def{
	{Key: "bot.stale", Default: "Кнопка устарела, вот главное меню"},
	{Key: "bot.too_fast", Default: "Слишком часто, подождите секунду"},
	{Key: "bot.banned", Default: "Ваш аккаунт заблокирован. Если это ошибка, напишите в поддержку."},
	{Key: "bot.maintenance", Default: "🛠 Идут технические работы. Загляните чуть позже, подписка продолжает работать."},
	{Key: "bot.menu", Default: "Главное меню"},
	{Key: "support.contact", Params: []string{"contact"}, Default: "Напишите в поддержку: {contact}"},
	{Key: "bot.error", Default: "Что-то пошло не так. Попробуйте ещё раз чуть позже."},
}
