package core

import (
	"context"
	"slices"
)

// Team отдаёт команду: владельца и помощников с правами. Только владелец. Владелец стоит первым и показан
// со всеми правами, даже если у него нет строки в таблице.
func (c *Core) Team(ctx context.Context, actorTg int64) ([]Admin, error) {
	list, err := c.Admins(ctx, actorTg)
	if err != nil {
		return nil, err
	}
	out := []Admin{{TgID: c.owner, Owner: true, Perms: HelperPerms}}
	for _, a := range list {
		if c.isOwner(a.TgID) {
			out[0].CreatedAt = a.CreatedAt
			continue
		}
		a.Perms = slices.Clone(a.Perms)
		out = append(out, a)
	}
	return out, nil
}
