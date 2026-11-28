package core

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/bini69-oi/worker/settings"
)

func init() {
	settings.SetCheck("summary.tz", func(v string) error {
		if _, err := time.LoadLocation(v); err != nil {
			return errors.New("неизвестный часовой пояс, пример Europe/Moscow")
		}
		return nil
	})
	settings.SetCheck("summary.hour", func(v string) error {
		if n, err := strconv.Atoi(v); err != nil || n < 0 || n > 23 {
			return errors.New("час от 0 до 23")
		}
		return nil
	})
}

// Location отдаёт часовой пояс дней в сводке (настройка summary.tz). Неразобранный пояс даёт UTC.
func (c *Core) Location() *time.Location {
	loc, err := time.LoadLocation(c.cfg.String("summary.tz"))
	if err != nil {
		return time.UTC
	}
	return loc
}

// SendSummary кладёт в тему «Сводка» итог вчерашнего дня: выручка, новые, пробные, первые оплаты. Зовётся задачей
// раз в минуту: итог уходит в первый запуск после часа из настройки summary.hour. Итог одного дня уходит один раз,
// отметка лежит в базе, поэтому перезапуск в 9:01 второй итог не шлёт. Отдаёт 1, если итог положен.
func (c *Core) SendSummary(ctx context.Context) (int, error) {
	now := c.clock.Now().In(c.Location())
	if now.Hour() < int(c.cfg.Int("summary.hour")) {
		return 0, nil
	}
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	yesterday := today.AddDate(0, 0, -1)
	// Итог за этот день уже ушёл: считать заново незачем, задача зовётся раз в минуту.
	done := false
	if err := c.store.Tx(ctx, func(tx Tx) error {
		var err error
		done, err = tx.HasNotice(0, "summary", yesterday.Format("2006-01-02"))
		return err
	}); err != nil || done {
		return 0, err
	}
	st, err := c.stats(ctx, yesterday, today)
	if err != nil {
		return 0, err
	}
	sent := 0
	err = c.tx(ctx, func(tx Tx) error {
		first, err := tx.AddNotice(0, "summary", yesterday.Format("2006-01-02"), now)
		if err != nil || !first {
			return err
		}
		sent = 1
		return enqueueAdmin(tx, AdminTask{Topic: TopicSummary, Key: "admin.summary", Vars: map[string]string{
			"date": yesterday.Format("02.01.2006"), "revenue": itoa(st.Revenue), "new": strconv.Itoa(st.NewUsers),
			"trials": strconv.Itoa(st.Trials), "first_paid": strconv.Itoa(st.FirstPaid)}}, now)
	})
	return sent, err
}
