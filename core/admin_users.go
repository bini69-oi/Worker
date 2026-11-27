package core

import (
	"context"
	"errors"
	"html"
	"slices"
	"strconv"
	"strings"
	"time"
)

// maxQuery это предел текста поиска: Telegram ID, код рефералки и имя короче.
const maxQuery = 64

// FindUsers ищет абонентов для админки. Нужно право users. Текст: число это Telegram ID (и код рефералки),
// `@имя` это начало имени без учёта регистра, остальное это точный код рефералки. Знаки `%` и `_` в имени
// ищутся как есть. Страницы по PageSize, cursor это Next прошлой страницы.
func (c *Core) FindUsers(ctx context.Context, actorTg int64, text, filter string, cursor int64) (Page[UserRow], error) {
	var page Page[UserRow]
	if _, err := c.Require(ctx, actorTg, PermUsers, "user.find"); err != nil {
		return page, err
	}
	text = strings.TrimSpace(text)
	if len([]rune(text)) > maxQuery {
		return page, ErrTooLong
	}
	if filter != "" && !slices.Contains(UserFilters, filter) {
		return page, ErrInvalid
	}
	q := UserQuery{Filter: filter, After: cursor}
	switch name, isName := strings.CutPrefix(text, "@"); {
	case isName:
		q.UsernamePrefix = name
	case text != "":
		q.RefCode = text
		if n, err := strconv.ParseInt(text, 10, 64); err == nil && n > 0 {
			q.TgID = n
		}
	}
	rows, err := c.store.FindUsers(ctx, q, c.clock.Now(), PageSize+1)
	if err != nil {
		return page, err
	}
	page.Items = rows
	if len(rows) > PageSize {
		page.Items = rows[:PageSize]
		page.Next = page.Items[PageSize-1].User.ID
	}
	return page, nil
}

// cardLimit это сколько движений, платежей и записей журнала видно в карточке.
const cardLimit = 50

// UserCard это всё об абоненте для админки.
type UserCard struct {
	User     User
	Referrer *User         // кто пригласил, nil если пришёл сам
	Sub      *Subscription // nil, если подписки нет
	Tariff   string        // название тарифа, у пробного пусто
	// NextTariff это название тарифа в ждущей смене, пусто если смены нет.
	NextTariff string
	Ledger     []LedgerEntry
	Payments   []Payment
	Audit      []AuditRow
	Devices    []PanelDevice
	// DevicesOK ложно, если панель не ответила: карточка всё равно открывается.
	DevicesOK bool
}

// UserCard собирает карточку абонента. Нужно право users. Устройства берутся у панели при открытии,
// сбой панели не роняет карточку.
func (c *Core) UserCard(ctx context.Context, actorTg, userID int64) (UserCard, error) {
	var card UserCard
	if _, err := c.Require(ctx, actorTg, PermUsers, "user.card"); err != nil {
		return card, err
	}
	u, err := c.store.User(ctx, userID)
	if err != nil {
		return card, err
	}
	card.User = u
	if u.ReferrerID != 0 {
		if r, err := c.store.User(ctx, u.ReferrerID); err == nil {
			card.Referrer = &r
		}
	}
	if sub, err := c.store.SubByUser(ctx, userID); err == nil {
		card.Sub = &sub
		if err := c.cardTariffs(ctx, &card, sub); err != nil {
			return card, err
		}
	} else if !errors.Is(err, ErrNotFound) {
		return card, err
	}
	if card.Ledger, err = c.store.Ledger(ctx, userID, cardLimit); err != nil {
		return card, err
	}
	if card.Payments, err = c.store.UserPayments(ctx, userID, cardLimit); err != nil {
		return card, err
	}
	if card.Audit, err = c.store.AuditLog(ctx, AuditFilter{Target: itoa(userID)}, cardLimit); err != nil {
		return card, err
	}
	card.Devices, card.DevicesOK = c.cardDevices(ctx, userID)
	return card, nil
}

// cardTariffs находит названия текущего тарифа и тарифа из ждущей смены.
func (c *Core) cardTariffs(ctx context.Context, card *UserCard, sub Subscription) error {
	all, err := c.store.Tariffs(ctx, false)
	if err != nil {
		return err
	}
	for _, t := range all {
		if t.ID == sub.TariffID {
			card.Tariff = t.Name
		}
		if sub.NextTariffID != 0 && t.ID == sub.NextTariffID {
			card.NextTariff = t.Name
		}
	}
	return nil
}

// cardDevices берёт устройства у панели. Нет подписки или её ещё нет в панели: пусто и без ошибки.
func (c *Core) cardDevices(ctx context.Context, userID int64) ([]PanelDevice, bool) {
	list, err := c.Devices(ctx, userID)
	switch {
	case err == nil:
		return list, true
	case errors.Is(err, ErrPanelPending), errors.Is(err, ErrNotFound):
		return nil, true
	}
	return nil, false
}

// maxMessage это предел сообщения Telegram.
const maxMessage = 4096

// SendToUser отправляет абоненту сообщение от бота через очередь. Нужно право support. Текст админа
// показывается как есть: разметка в нём экранируется. Абонент, заблокировавший бота, даёт ErrUnreachable.
func (c *Core) SendToUser(ctx context.Context, actorTg, userID int64, text string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return ErrInvalid
	}
	if len([]rune(text)) > maxMessage {
		return ErrTooLong
	}
	return c.asAdmin(ctx, actorTg, PermSupport, "user.message", func(tx Tx, _ Admin, now time.Time) (string, map[string]string, error) {
		u, err := tx.User(userID)
		if err != nil {
			return "", nil, err
		}
		if !u.UnreachableAt.IsZero() {
			return "", nil, ErrUnreachable
		}
		task := NotifyTask{UserID: userID, Key: "notify.support_reply", Vars: map[string]string{"text": html.EscapeString(text)}}
		if err := enqueueNotify(tx, task, now); err != nil {
			return "", nil, err
		}
		short := []rune(text)
		return itoa(userID), map[string]string{"text": string(short[:min(len(short), 200)])}, nil
	})
}
