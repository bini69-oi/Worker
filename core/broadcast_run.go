package core

import (
	"context"
	"errors"
	"time"
)

// Предел отправки: 25 сообщений в секунду, с запасом под общий предел Telegram (30 в секунду на бота).
const (
	broadcastRate  = 25
	broadcastEvery = time.Second
)

// RunBroadcast отправляет очередную пачку идущей рассылки: не больше broadcastRate сообщений, абоненты по
// возрастанию id. Зовётся задачей раз в секунду; чаще раза в секунду пачка не уходит, поэтому предел держится
// сам, а не зависит от планировщика. После каждого сообщения курсор и счётчики сохраняются в базе: после
// перезапуска рассылка продолжается с курсора, и дважды получить сообщение может не больше одного абонента
// на границе. Ответ 429 откладывает следующую пачку на retry_after. Отмена видна после текущего сообщения.
// Отдаёт число отправленных сообщений.
func (c *Core) RunBroadcast(ctx context.Context) (int, error) {
	now := c.clock.Now()
	if c.sender == nil || now.Before(c.bcNext.Load().(time.Time)) {
		return 0, nil
	}
	b, err := c.runningBroadcast(ctx)
	if err != nil || b.ID == 0 {
		return 0, err
	}
	c.bcNext.Store(now.Add(broadcastEvery))
	users, err := c.store.SegmentUsers(ctx, b.Segment, b.CursorUserID, now, broadcastRate)
	if err != nil {
		return 0, err
	}
	if len(users) == 0 {
		return 0, c.finishBroadcast(ctx, b.ID)
	}
	sent := 0
	for i, u := range users {
		// Отмену между сообщениями видно сразу: после неё никому ничего не уходит.
		if i > 0 {
			if cur, err := c.runningBroadcast(ctx); err != nil || cur.ID == 0 {
				return sent, err
			}
		}
		stop, err := c.sendOne(ctx, b, u)
		if stop || err != nil {
			return sent, err
		}
		sent++
	}
	return sent, nil
}

func (c *Core) runningBroadcast(ctx context.Context) (Broadcast, error) {
	var b Broadcast
	err := c.store.Tx(ctx, func(tx Tx) error {
		var err error
		b, err = tx.RunningBroadcast()
		if errors.Is(err, ErrNotFound) {
			return nil
		}
		return err
	})
	return b, err
}

// sendOne отправляет одному абоненту и записывает итог вместе с курсором. stop говорит, что пачку пора
// закончить: рассылку отменили, Telegram просит подождать или процесс останавливается.
func (c *Core) sendOne(ctx context.Context, b Broadcast, u User) (stop bool, err error) {
	var wait *RetryAfter
	sendErr := c.sender.Send(ctx, u.TgID, b.Text, b.ButtonText, b.ButtonURL)
	var sent, failed, blocked int
	switch {
	case sendErr == nil:
		sent = 1
	case errors.Is(sendErr, ErrRecipientGone):
		blocked = 1
	case errors.Is(sendErr, ErrMarkupRejected):
		return true, c.stopBroadcast(ctx, b, u)
	case errors.As(sendErr, &wait):
		c.bcNext.Store(c.clock.Now().Add(time.Duration(max(wait.Seconds, 1)) * time.Second))
		return true, nil // этого абонента повторим в следующей пачке: курсор не двигаем
	case ctx.Err() != nil:
		return true, ctx.Err()
	default:
		c.log.Warn("сообщение рассылки не отправилось", "broadcast", b.ID, "user", u.ID, "err", c.shortErr(sendErr))
		failed = 1
	}
	running := true
	err = c.tx(ctx, func(tx Tx) error {
		var err error
		if running, err = tx.BroadcastStep(b.ID, u.ID, sent, failed, blocked); err != nil || blocked == 0 {
			return err
		}
		return tx.SetUnreachable(u.ID, c.clock.Now())
	})
	return !running, err
}

func (c *Core) finishBroadcast(ctx context.Context, id int64) error {
	return c.tx(ctx, func(tx Tx) error {
		now := c.clock.Now()
		ok, err := tx.FinishBroadcast(id, now)
		if err != nil || !ok {
			return err
		}
		b, err := tx.Broadcast(id)
		if err != nil {
			return err
		}
		return enqueueAdmin(tx, AdminTask{Topic: TopicSummary, Key: "admin.broadcast_done", Vars: map[string]string{
			"sent": itoa(int64(b.Sent)), "failed": itoa(int64(b.Failed)), "blocked": itoa(int64(b.Blocked)), "total": itoa(int64(b.Total))}}, now)
	})
}

// stopBroadcast останавливает рассылку, у которой Telegram отверг разметку: статус «отменена», отказ в счётчике
// ошибок, админам уведомление в тему «Ошибки». Остальные получатели ничего не получают.
func (c *Core) stopBroadcast(ctx context.Context, b Broadcast, u User) error {
	return c.tx(ctx, func(tx Tx) error {
		now := c.clock.Now()
		if _, err := tx.BroadcastStep(b.ID, u.ID, 0, 1, 0); err != nil {
			return err
		}
		if ok, err := tx.CancelBroadcast(b.ID, now); err != nil || !ok {
			return err
		}
		return enqueueAdmin(tx, AdminTask{Topic: TopicErrors, Key: "admin.broadcast_rejected",
			Vars: map[string]string{"id": itoa(b.ID)}}, now)
	})
}
