package core

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ExpireDue отключает подписки, срок которых вышел, ставит задачу отключить абонента в панели
// и говорит абоненту, что подписка закончилась.
// Возвращает, сколько подписок отключено. Повторный запуск ничего не делает.
func (c *Core) ExpireDue(ctx context.Context) (int, error) {
	n := 0
	err := c.tx(ctx, func(tx Tx) error {
		now := c.clock.Now()
		subs, err := tx.ActiveSubsUntil(now)
		if err != nil {
			return err
		}
		for _, s := range subs {
			if s.NextTariffID != 0 {
				switched, err := c.startPlan(tx, s, now)
				if err != nil {
					return err
				}
				if switched {
					continue
				}
				if s, err = tx.Sub(s.ID); err != nil { // смена отменена: истекает уже без неё
					return err
				}
			}
			s.ExtraTrafficBytes, s.ExtraDevices = 0, 0 // докупленное живёт, пока идёт подписка
			s.Status, s.UpdatedAt = SubExpired, now
			if _, err := tx.SaveSub(s); err != nil {
				return err
			}
			if err := enqueueSync(tx, s.ID, now); err != nil {
				return err
			}
			// Статус «истекла» ставится один раз за срок, поэтому и сообщение уходит один раз.
			vars := map[string]string{"trial": "0"}
			if s.Trial {
				vars["trial"] = "1"
			}
			if err := enqueueNotify(tx, NotifyTask{UserID: s.UserID, Key: "notify.expired", Vars: vars}, now); err != nil {
				return err
			}
			n++
		}
		return nil
	})
	return n, err
}

// RemindDue напоминает об окончании подписки. Пороги в днях берутся из настройки remind.days (например «3,1»).
// Абонент получает напоминание с ближайшим порогом, не меньшим оставшихся дней, и только один раз на порог
// и дату окончания: после продления напоминания начнутся заново.
func (c *Core) RemindDue(ctx context.Context) (int, error) {
	marks := remindDays(c.cfg.String("remind.days"))
	if len(marks) == 0 {
		return 0, nil
	}
	n := 0
	err := c.tx(ctx, func(tx Tx) error {
		now := c.clock.Now()
		subs, err := tx.ActiveSubsUntil(now.Add(time.Duration(marks[len(marks)-1]) * day))
		if err != nil {
			return err
		}
		for _, s := range subs {
			left := s.ExpiresAt.Sub(now)
			if left <= 0 {
				continue
			}
			mark := pickMark(marks, int(math.Ceil(left.Hours()/24)))
			ref := fmt.Sprintf("%d:%d:%d", s.ID, s.ExpiresAt.Unix(), mark)
			fresh, err := tx.AddNotice(s.UserID, "remind", ref, now)
			if err != nil {
				return err
			}
			if !fresh {
				continue
			}
			task := NotifyTask{UserID: s.UserID, Key: "notify.remind", Vars: map[string]string{"days": strconv.Itoa(mark)}}
			if err := enqueueNotify(tx, task, now); err != nil {
				return err
			}
			n++
		}
		return nil
	})
	return n, err
}

// remindDays разбирает «3,1» в отсортированный список [1 3]. Мусор и неположительные числа пропускаются.
func remindDays(raw string) []int {
	var out []int
	seen := map[int]bool{}
	for _, part := range strings.Split(raw, ",") {
		d, err := strconv.Atoi(strings.TrimSpace(part))
		if err == nil && d > 0 && !seen[d] {
			out = append(out, d)
			seen[d] = true
		}
	}
	sort.Ints(out)
	return out
}

// pickMark берёт наименьший порог, который не меньше оставшихся дней. marks отсортированы.
func pickMark(marks []int, daysLeft int) int {
	for _, m := range marks {
		if m >= daysLeft {
			return m
		}
	}
	return marks[len(marks)-1]
}
