package core

import (
	"context"
	"errors"
)

// panelSub отдаёт подписку абонента, у которой уже есть абонент в панели.
func (c *Core) panelSub(ctx context.Context, userID int64) (Subscription, error) {
	s, err := c.store.SubByUser(ctx, userID)
	if err != nil {
		return s, err
	}
	if s.PanelUUID == "" {
		return s, ErrPanelPending
	}
	return s, nil
}

// panelErr прячет ошибку панели от абонента за понятным кодом, а саму ошибку пишет в журнал.
func (c *Core) panelErr(op string, err error) error {
	if err == nil || errors.Is(err, ErrPanelPending) || errors.Is(err, ErrNotFound) {
		return err
	}
	c.log.Warn("панель не ответила", "op", op, "err", err)
	return ErrPanelDown
}

// Devices отдаёт устройства абонента из панели.
func (c *Core) Devices(ctx context.Context, userID int64) ([]PanelDevice, error) {
	s, err := c.panelSub(ctx, userID)
	if err != nil {
		return nil, err
	}
	list, err := c.panel.Devices(ctx, s.PanelUUID)
	return list, c.panelErr("devices", err)
}

// DeleteDevice отвязывает одно устройство абонента.
func (c *Core) DeleteDevice(ctx context.Context, userID int64, hwid string) error {
	if err := c.serving(ctx, userID); err != nil {
		return err
	}
	if hwid == "" {
		return ErrInvalid
	}
	s, err := c.panelSub(ctx, userID)
	if err != nil {
		return err
	}
	return c.panelErr("delete_device", c.panel.DeleteDevice(ctx, s.PanelUUID, hwid))
}

// ResetDevices отвязывает все устройства абонента.
func (c *Core) ResetDevices(ctx context.Context, userID int64) error {
	if err := c.serving(ctx, userID); err != nil {
		return err
	}
	s, err := c.panelSub(ctx, userID)
	if err != nil {
		return err
	}
	return c.panelErr("reset_devices", c.panel.ResetDevices(ctx, s.PanelUUID))
}

// RevokeLink перевыпускает ссылку подписки. Старая перестаёт работать, новая сохраняется в базе.
func (c *Core) RevokeLink(ctx context.Context, userID int64) (Subscription, error) {
	if err := c.serving(ctx, userID); err != nil {
		return Subscription{}, err
	}
	s, err := c.panelSub(ctx, userID)
	if err != nil {
		return s, err
	}
	res, err := c.panel.RevokeLink(ctx, s.PanelUUID)
	if err != nil {
		return s, c.panelErr("revoke", err)
	}
	err = c.tx(ctx, func(tx Tx) error {
		fresh, err := tx.Sub(s.ID)
		if err != nil {
			return err
		}
		fresh.SubURL = res.SubURL
		_, err = tx.SaveSub(fresh)
		s = fresh
		return err
	})
	return s, err
}
