package core

import (
	"context"
	"sort"
	"strings"
	"time"
)

// GatewayState это касса для экрана: включена ли, заполнены ли ключи, порядок, последняя оплата и ждущие платежи.
type GatewayState struct {
	Name       string
	Title      string
	Enabled    bool
	Configured bool // ключи заполнены
	Sort       int64
	LastPaidAt time.Time // нулевое время, если оплат ещё не было
	Pending    int
}

// GatewayStates отдаёт состояние всех касс. Только владелец.
func (c *Core) GatewayStates(ctx context.Context, actorTg int64) ([]GatewayState, error) {
	if _, err := c.Require(ctx, actorTg, PermOwner, "gateway.list"); err != nil {
		return nil, err
	}
	stats, err := c.store.GatewayStats(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]GatewayState, 0, len(c.gateways))
	for _, g := range c.gateways {
		n := g.Name()
		st := stats[n]
		out = append(out, GatewayState{Name: n, Title: c.cfg.String(n + ".title"), Enabled: c.cfg.Bool(n + ".enabled"),
			Configured: g.Configured(), Sort: c.cfg.Int(n + ".sort"), LastPaidAt: st.LastPaidAt, Pending: st.Pending})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Sort < out[j].Sort })
	return out, nil
}

// checkGatewayKeys не даёт включить кассу без ключей: абонент увидел бы кассу, которая не работает.
func (c *Core) checkGatewayKeys(p settingPayload) error {
	name, ok := strings.CutSuffix(p.Key, ".enabled")
	if !ok || p.Reset || p.Value != "true" {
		return nil
	}
	if g, found := c.gateway(name); found && !g.Configured() {
		return ErrNoKeys
	}
	return nil
}
