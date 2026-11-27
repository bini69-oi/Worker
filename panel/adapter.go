package panel

import (
	"context"

	"github.com/bini69-oi/worker/core"
)

// CoreAdapter подключает клиента панели к ядру: переводит типы ядра в типы панели и обратно.
type CoreAdapter struct {
	Client *Client
}

var _ core.Panel = CoreAdapter{}

func toCore(r Result) core.PanelResult {
	return core.PanelResult{Ref: r.PanelID, SubURL: r.SubscriptionURL, ExpireAt: r.ExpireAt, Enabled: r.Enabled}
}

// coreErr переводит «не найдено» панели в ошибку ядра, остальное отдаёт как есть.
func coreErr(err error) error {
	if IsNotFound(err) {
		return core.ErrNotFound
	}
	return err
}

func (a CoreAdapter) Sync(ctx context.Context, u core.PanelUser) (core.PanelResult, error) {
	res, err := a.Client.Sync(ctx, State{
		PanelID: u.Ref, Username: u.Username, TelegramID: u.TgID, ExpireAt: u.ExpireAt,
		TrafficLimitBytes: u.TrafficBytes, Strategy: Strategy(u.Reset), DeviceLimit: u.Devices,
		Squads: u.Squads, Enabled: u.Enabled, Description: u.Description,
	})
	return toCore(res), coreErr(err)
}

func (a CoreAdapter) Get(ctx context.Context, ref string) (core.PanelResult, error) {
	res, err := a.Client.Get(ctx, ref)
	return toCore(res), coreErr(err)
}

func (a CoreAdapter) Devices(ctx context.Context, ref string) ([]core.PanelDevice, error) {
	list, err := a.Client.Devices(ctx, ref)
	out := make([]core.PanelDevice, 0, len(list))
	for _, d := range list {
		out = append(out, core.PanelDevice{HWID: d.HWID, Platform: d.Platform, Model: d.Model, CreatedAt: d.CreatedAt})
	}
	return out, coreErr(err)
}

func (a CoreAdapter) DeleteDevice(ctx context.Context, ref, hwid string) error {
	return coreErr(a.Client.DeleteDevice(ctx, ref, hwid))
}

func (a CoreAdapter) ResetDevices(ctx context.Context, ref string) error {
	return coreErr(a.Client.ResetDevices(ctx, ref))
}

func (a CoreAdapter) RevokeLink(ctx context.Context, ref string) (core.PanelResult, error) {
	res, err := a.Client.RevokeLink(ctx, ref)
	return toCore(res), coreErr(err)
}

// EachUser читает пользователей панели наибольшими страницами и отдаёт их ядру для импорта.
func (a CoreAdapter) EachUser(ctx context.Context, fn func(core.PanelPage) error) error {
	return a.Client.EachUsersPage(ctx, MaxPageSize, func(p UsersPage) error {
		out := core.PanelPage{Users: make([]core.PanelEntry, 0, len(p.Users))}
		for _, u := range p.Users {
			out.Users = append(out.Users, core.PanelEntry{Ref: u.PanelID, Username: u.Username, TgID: u.TelegramID,
				Status: u.Status, ExpireAt: u.ExpireAt, CreatedAt: u.CreatedAt, SubURL: u.SubURL,
				TrafficBytes: u.TrafficLimitBytes, Devices: u.DeviceLimit, Reset: string(u.TrafficReset), Squads: u.Squads})
		}
		for _, s := range p.Skipped {
			out.Skipped = append(out.Skipped, core.PanelSkip{Ref: s.PanelID, Username: s.Username, Reason: s.Reason})
		}
		return fn(out)
	})
}

// Squads отдаёт сквады панели ядру: админка по ним предлагает выбор в тарифе.
func (a CoreAdapter) Squads(ctx context.Context) ([]core.PanelSquad, error) {
	list, err := a.Client.Squads(ctx)
	out := make([]core.PanelSquad, 0, len(list))
	for _, s := range list {
		out = append(out, core.PanelSquad{UUID: s.UUID, Name: s.Name, Members: s.Members})
	}
	return out, coreErr(err)
}
