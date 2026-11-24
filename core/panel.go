package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// PanelUser это состояние абонента, к которому ядро хочет привести панель.
type PanelUser struct {
	Ref          string // идентификатор в панели, пустой, если абонента там ещё нет
	Username     string
	TgID         int64
	ExpireAt     time.Time
	TrafficBytes int64 // 0 значит без лимита
	Reset        string
	Devices      int // 0 значит без лимита
	Squads       []string
	Enabled      bool
	Description  string
}

// PanelResult это то, что панель знает об абоненте.
type PanelResult struct {
	Ref      string
	SubURL   string
	ExpireAt time.Time
	Enabled  bool
}

// PanelDevice это устройство абонента в панели.
type PanelDevice struct {
	HWID      string
	Platform  string
	Model     string
	CreatedAt time.Time
}

// Panel это панель Remnawave глазами ядра. Реализует её пакет panel, в тестах стоит фейк.
type Panel interface {
	// Sync приводит абонента к состоянию u. Повторный вызов с тем же u ничего не меняет.
	Sync(ctx context.Context, u PanelUser) (PanelResult, error)
	Get(ctx context.Context, ref string) (PanelResult, error)
	Devices(ctx context.Context, ref string) ([]PanelDevice, error)
	DeleteDevice(ctx context.Context, ref, hwid string) error
	ResetDevices(ctx context.Context, ref string) error
	RevokeLink(ctx context.Context, ref string) (PanelResult, error)
}

const gib = 1 << 30

// panelUser собирает состояние абонента в панели из подписки. Пробный период берёт лимиты из настроек,
// платная подписка из тарифа.
func (c *Core) panelUser(s Subscription, u User, t Tariff, now time.Time) PanelUser {
	p := PanelUser{
		Ref:         s.PanelUUID,
		Username:    fmt.Sprintf("u%d", u.TgID),
		TgID:        u.TgID,
		ExpireAt:    s.ExpiresAt,
		Enabled:     s.Status == SubActive && s.ExpiresAt.After(now) && u.BannedAt.IsZero(),
		Description: fmt.Sprintf("worker user %d", u.ID),
	}
	if s.Trial {
		p.TrafficBytes = c.cfg.Int("trial.traffic_gb") * gib
		p.Devices = int(c.cfg.Int("trial.devices"))
		p.Reset = "NO_RESET"
		p.Squads = splitList(c.cfg.String("trial.squads"))
		return p
	}
	p.TrafficBytes, p.Devices, p.Reset, p.Squads = t.TrafficBytes, t.Devices, t.Reset, t.Squads
	return p
}

// syncSub приводит панель к состоянию подписки из базы и запоминает ссылку и идентификатор в панели.
// Состояние читается в момент выполнения, поэтому повтор задачи безопасен.
func (c *Core) syncSub(ctx context.Context, subID int64) error {
	var want PanelUser
	var sub Subscription
	err := c.store.Tx(ctx, func(tx Tx) error {
		var err error
		if sub, err = tx.Sub(subID); err != nil {
			return err
		}
		u, err := tx.User(sub.UserID)
		if err != nil {
			return err
		}
		var t Tariff
		if !sub.Trial {
			if t, err = tx.Tariff(sub.TariffID); err != nil {
				return err
			}
		}
		want = c.panelUser(sub, u, t, c.clock.Now())
		return nil
	})
	if errors.Is(err, ErrNotFound) {
		return nil // подписку удалили, синхронизировать нечего
	}
	if err != nil {
		return err
	}

	res, err := c.panel.Sync(ctx, want)
	if err != nil {
		return err
	}
	if res.Ref == sub.PanelUUID && res.SubURL == sub.SubURL {
		return nil
	}
	return c.store.Tx(ctx, func(tx Tx) error {
		s, err := tx.Sub(subID)
		if err != nil {
			return err
		}
		s.PanelUUID, s.SubURL = res.Ref, res.SubURL
		_, err = tx.SaveSub(s)
		return err
	})
}

func runPanelSync(ctx context.Context, c *Core, payload []byte) error {
	var t PanelSyncTask
	if err := json.Unmarshal(payload, &t); err != nil {
		return err
	}
	return c.syncSub(ctx, t.SubID)
}

// splitList разбирает «a, b,,c» в [a b c].
func splitList(raw string) []string {
	var out []string
	for _, part := range strings.Split(raw, ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}
