package core

import "context"

// Audit отдаёт журнал действий админов страницами по PageSize, новые первыми, с фильтром по админу, действию
// и датам. Только владелец. Журнал только читается: правки и удаления из админки нет. Значения проходят через
// фильтр секретов: даже ошибка автора, положившего ключ в подробности, наружу его не выпустит.
func (c *Core) Audit(ctx context.Context, actorTg int64, f AuditFilter, cursor int64) (Page[AuditRow], error) {
	var page Page[AuditRow]
	if _, err := c.Require(ctx, actorTg, PermOwner, "audit.list"); err != nil {
		return page, err
	}
	f.After = cursor
	rows, err := c.store.AuditLog(ctx, f, PageSize+1)
	if err != nil {
		return page, err
	}
	page.Items = rows
	if len(rows) > PageSize {
		page.Items = rows[:PageSize]
		page.Next = page.Items[PageSize-1].ID
	}
	c.cleanAudit(page.Items)
	return page, nil
}

// cleanAudit пропускает значения записей журнала через фильтр секретов.
func (c *Core) cleanAudit(rows []AuditRow) {
	if c.filter == nil {
		return
	}
	for i, r := range rows {
		clean := make(map[string]string, len(r.Details))
		for k, v := range r.Details {
			clean[k] = c.filter.String(v)
		}
		rows[i].Details = clean
	}
}
