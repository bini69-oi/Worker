package core

import (
	"context"
	"time"
)

// checkChannel пускает к пробному периоду только подписчиков канала из trial.channel.
// Если Telegram не ответил или бот не админ канала, абонент проходит, а админ раз в час получает
// событие в тему «Ошибки»: кривая настройка не должна останавливать продажи.
func (c *Core) checkChannel(ctx context.Context, userID int64) error {
	channel := c.cfg.String("trial.channel")
	if channel == "" || c.channels == nil {
		return nil
	}
	u, err := c.store.User(ctx, userID)
	if err != nil || u.TrialUsed {
		return err // кто уже брал пробный, услышит об этом, а не о канале
	}
	ok, checkErr := c.channels.IsMember(ctx, channel, u.TgID)
	if checkErr == nil {
		if !ok {
			return ErrNotSubscribed
		}
		return nil
	}
	c.log.Warn("не проверить подписку на канал", "channel", channel, "err", checkErr)
	now := c.clock.Now()
	_ = c.tx(ctx, func(tx Tx) error {
		first, err := tx.AddNotice(0, "channel_check", now.Truncate(time.Hour).Format(time.RFC3339), now)
		if err != nil || !first {
			return err
		}
		return enqueueAdmin(tx, AdminTask{Topic: TopicErrors, Key: "admin.channel_check",
			Vars: map[string]string{"channel": channel, "error": c.shortErr(checkErr)}}, now)
	})
	return nil
}
