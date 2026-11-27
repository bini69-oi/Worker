package core

import (
	"context"
	"slices"
	"strconv"
	"strings"
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
