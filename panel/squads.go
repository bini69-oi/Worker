package panel

import (
	"context"
	"net/http"
)

// Squad это внутренний сквад панели: группа серверов, которую тариф выдаёт абоненту.
type Squad struct {
	UUID    string
	Name    string
	Members int
}

// Squads читает внутренние сквады панели. Только чтение: админка по ним рисует галочки в тарифе.
func (c *Client) Squads(ctx context.Context) ([]Squad, error) {
	var out struct {
		Squads []struct {
			UUID string `json:"uuid"`
			Name string `json:"name"`
			Info struct {
				Members int `json:"membersCount"`
			} `json:"info"`
		} `json:"internalSquads"`
	}
	if err := c.do(ctx, http.MethodGet, "/api/internal-squads", nil, &out); err != nil {
		return nil, err
	}
	list := make([]Squad, 0, len(out.Squads))
	for _, s := range out.Squads {
		list = append(list, Squad{UUID: s.UUID, Name: s.Name, Members: s.Info.Members})
	}
	return list, nil
}
