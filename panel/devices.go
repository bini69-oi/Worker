package panel

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"time"
)

// Devices возвращает устройства абонента, привязанные в панели.
func (c *Client) Devices(ctx context.Context, ref string) ([]Device, error) {
	if ref == "" {
		return nil, errors.New("panel: не указан абонент")
	}
	var out struct {
		Devices []struct {
			HWID      string    `json:"hwid"`
			Platform  string    `json:"platform"`
			Model     string    `json:"deviceModel"`
			CreatedAt time.Time `json:"createdAt"`
		} `json:"devices"`
	}
	if err := c.do(ctx, http.MethodGet, "/api/hwid/devices/"+url.PathEscape(ref), nil, &out); err != nil {
		return nil, err
	}
	list := make([]Device, 0, len(out.Devices))
	for _, d := range out.Devices {
		list = append(list, Device{HWID: d.HWID, Platform: d.Platform, Model: d.Model, CreatedAt: d.CreatedAt})
	}
	return list, nil
}

// DeleteDevice отвязывает одно устройство.
func (c *Client) DeleteDevice(ctx context.Context, ref, hwid string) error {
	ct, err := c.current(ctx)
	if err != nil {
		return err
	}
	if !ct.validRef(ref) || hwid == "" {
		return errors.New("panel: не указан абонент или устройство")
	}
	return c.do(ctx, http.MethodPost, "/api/hwid/devices/delete", ct.deviceBody(ref, hwid), nil)
}

// ResetDevices отвязывает все устройства абонента одним запросом. Повтор безопасен.
func (c *Client) ResetDevices(ctx context.Context, ref string) error {
	ct, err := c.current(ctx)
	if err != nil {
		return err
	}
	if !ct.validRef(ref) {
		return errors.New("panel: не указан абонент")
	}
	return c.do(ctx, http.MethodPost, "/api/hwid/devices/delete-all", ct.deviceBody(ref, ""), nil)
}

// RevokeLink перевыпускает ссылку подписки. Старая ссылка перестаёт работать.
// Возвращает абонента с новой ссылкой в SubscriptionURL.
func (c *Client) RevokeLink(ctx context.Context, ref string) (Result, error) {
	ct, err := c.current(ctx)
	if err != nil {
		return Result{}, err
	}
	if !ct.validRef(ref) {
		return Result{}, errors.New("panel: не указан абонент")
	}
	var u wireUser
	if err := c.do(ctx, http.MethodPost, userPath(ref)+"/actions/revoke", map[string]any{}, &u); err != nil {
		return Result{}, err
	}
	return ct.result(u, false), nil
}
