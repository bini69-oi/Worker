package settings

import (
	"errors"
	"fmt"
	"strconv"
	"time"
)

// Kind это тип настройки.
type Kind int

const (
	KindBool     Kind = iota // true или false
	KindInt                  // целое число
	KindString               // строка
	KindDuration             // промежуток времени, например 72h
	KindMoney                // деньги в копейках, целое число
)

func (k Kind) String() string {
	switch k {
	case KindBool:
		return "bool"
	case KindInt:
		return "int"
	case KindString:
		return "string"
	case KindDuration:
		return "duration"
	case KindMoney:
		return "money"
	}
	return "unknown"
}

// Def описывает одну настройку. Настроек вне реестра не существует.
type Def struct {
	Key     string
	Kind    Kind
	Default string // умолчание в том же виде, в каком значение хранится в базе
	Desc    string
	Group   string
	Secret  bool // секрет хранится в базе зашифрованным и наружу целиком не отдаётся
}

// Ошибки, по которым вызывающий отличает отказы.
var (
	ErrUnknownKey = errors.New("настройки с таким ключом нет")
	ErrBadValue   = errors.New("значение не подходит к типу настройки")
)

// registry это весь список настроек. Новые ключи добавляют сюда.
var registry = base

// AddGateway добавляет в реестр общие настройки кассы (включена, название, порядок, пределы) и её собственные,
// например ключи. Касса зовёт его в init своего файла, поэтому новая касса не правит этот реестр.
// Зовётся только при старте программы, до чтения настроек.
func AddGateway(name, title string, extra ...Def) {
	for _, d := range append(gatewayDefs(name, title), extra...) {
		if _, ok := find(d.Key); !ok {
			registry = append(registry, d)
		}
	}
}

// gatewayDefs описывает общие настройки кассы: включена, название, порядок, пределы суммы.
func gatewayDefs(name, title string) []Def {
	g := "pay." + name
	return []Def{
		{Key: name + ".enabled", Kind: KindBool, Default: "false", Group: g, Desc: "Показывать ли кассу абонентам"},
		{Key: name + ".title", Kind: KindString, Default: title, Group: g, Desc: "Название кассы для абонента"},
		{Key: name + ".sort", Kind: KindInt, Default: "0", Group: g, Desc: "Порядок в списке касс, меньше значит выше"},
		{Key: name + ".min", Kind: KindMoney, Default: "0", Group: g, Desc: "Минимальная сумма пополнения, копейки, 0 без предела"},
		{Key: name + ".max", Kind: KindMoney, Default: "0", Group: g, Desc: "Максимальная сумма пополнения, копейки, 0 без предела"},
	}
}

var base = []Def{
	{Key: "pay.pending_hours", Kind: KindInt, Default: "24", Group: "pay",
		Desc: "Через сколько часов неоплаченный платёж считается истёкшим"},
	{Key: "trial.enabled", Kind: KindBool, Default: "true", Group: "trial",
		Desc: "Выдавать ли пробный период новым абонентам"},
	{Key: "trial.days", Kind: KindInt, Default: "3", Group: "trial",
		Desc: "Сколько дней длится пробный период"},
	{Key: "trial.traffic_gb", Kind: KindInt, Default: "5", Group: "trial",
		Desc: "Лимит трафика пробного периода, ГБ"},
	{Key: "trial.devices", Kind: KindInt, Default: "1", Group: "trial",
		Desc: "Сколько устройств можно подключить в пробный период"},
	{Key: "trial.squads", Kind: KindString, Default: "", Group: "trial",
		Desc: "Сквады пробного периода через запятую (UUID групп серверов в Remnawave)"},
	{Key: "autorenew.days_before", Kind: KindInt, Default: "1", Group: "notify",
		Desc: "За сколько дней до окончания продлевать подписку с баланса"},
	{Key: "ref.first_percent", Kind: KindInt, Default: "10", Group: "referral",
		Desc: "Процент пригласившему с первой покупки приглашённого"},
	{Key: "ref.first_fixed", Kind: KindMoney, Default: "0", Group: "referral",
		Desc: "Фиксированный бонус пригласившему за первую покупку приглашённого, копейки"},
	{Key: "ref.each_percent", Kind: KindInt, Default: "0", Group: "referral",
		Desc: "Процент пригласившему с каждой следующей покупки приглашённого, 0 выключено"},
	{Key: "ref.welcome_bonus", Kind: KindMoney, Default: "0", Group: "referral",
		Desc: "Бонус приглашённому при первом входе, копейки"},
	{Key: "remind.days", Kind: KindString, Default: "3,1", Group: "notify",
		Desc: "За сколько дней до окончания напоминать, через запятую"},
	{Key: "stars.rate", Kind: KindMoney, Default: "150", Group: "pay",
		Desc: "Сколько копеек стоит одна звезда Telegram"},
	{Key: "lang.default", Kind: KindString, Default: "ru", Group: "texts",
		Desc: "Язык текстов, если нужного перевода нет"},
	{Key: "http.addr", Kind: KindString, Default: ":8080", Group: "http",
		Desc: "Адрес, на котором Worker принимает вебхуки (за обратным прокси с https)"},
	{Key: "bot.token", Kind: KindString, Default: "", Group: "bot", Secret: true,
		Desc: "Новый токен бота после смены из админки. Пустой значит токен из .env"},
	{Key: "panel.reconcile_minutes", Kind: KindInt, Default: "60", Group: "panel",
		Desc: "Как часто сверять базу с панелью, минуты"},
	{Key: "panel.webhook_secret", Kind: KindString, Default: "", Group: "panel", Secret: true,
		Desc: "Секрет вебхуков панели (WEBHOOK_SECRET_HEADER в настройках Remnawave)"},
	{Key: "yookassa.secret_key", Kind: KindString, Default: "", Group: "pay", Secret: true,
		Desc: "Секретный ключ кассы ЮKassa"},
}

// Registry возвращает копию списка настроек.
func Registry() []Def {
	return append([]Def(nil), registry...)
}

func find(key string) (Def, bool) {
	for _, d := range registry {
		if d.Key == key {
			return d, true
		}
	}
	return Def{}, false
}

// normalize проверяет значение по типу и приводит к одному виду записи.
// Значение в текст ошибки не попадает: оно может оказаться секретом.
func normalize(k Kind, raw string) (string, error) {
	bad := fmt.Errorf("%w: нужен тип %s", ErrBadValue, k)
	switch k {
	case KindBool:
		b, err := strconv.ParseBool(raw)
		if err != nil {
			return "", bad
		}
		return strconv.FormatBool(b), nil
	case KindInt:
		n, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return "", bad
		}
		return strconv.FormatInt(n, 10), nil
	case KindMoney:
		n, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || n < 0 {
			return "", bad
		}
		return strconv.FormatInt(n, 10), nil
	case KindDuration:
		d, err := time.ParseDuration(raw)
		if err != nil || d < 0 {
			return "", bad
		}
		return d.String(), nil
	case KindString:
		return raw, nil
	}
	return "", bad
}
