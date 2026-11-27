package core

import (
	"context"
	"sort"
	"time"
)

// maxStatsDays это самый длинный период сводки: на большой базе дольше считать нечего, а смотреть полгода по дням незачем.
const maxStatsDays = 90

// DayStat это выручка одного дня за вычетом возвратов этого дня.
type DayStat struct {
	Day      time.Time
	Gross    int64 // оплачено
	Refunded int64 // возвращено в этот день
	Revenue  int64 // Gross - Refunded
}

// GatewayRevenue это выручка кассы за период за вычетом возвратов.
type GatewayRevenue struct {
	Gateway string
	Revenue int64
}

// Stats это сводка за период.
type Stats struct {
	From, To   time.Time
	Days       []DayStat
	Gateways   []GatewayRevenue
	Revenue    int64 // всего за период, копейки
	NewUsers   int
	Trials     int
	FirstPaid  int
	ActiveSubs int // на конец периода (или на сейчас, если период ещё идёт)
	StuckTasks int
	StuckUsers []int64 // абоненты, у которых зависла очередь панели
}

// Stats считает сводку за период [from, to): выручку по дням и кассам (зачисленные платежи за вычетом возвратов,
// копейки), новых абонентов, взятые пробные периоды, оплативших впервые, активные подписки на конец периода
// и зависшие задачи очереди. Нужно право stats. Период до 90 дней, иначе ErrPeriodTooLong и ничего не считается.
// Дни считаются в часовом поясе from.
func (c *Core) Stats(ctx context.Context, actorTg int64, from, to time.Time) (Stats, error) {
	st := Stats{From: from, To: to}
	if _, err := c.Require(ctx, actorTg, PermStats, "stats.get"); err != nil {
		return st, err
	}
	if !to.After(from) {
		return st, ErrInvalid
	}
	if to.Sub(from) > maxStatsDays*24*time.Hour {
		return st, ErrPeriodTooLong
	}
	now := c.clock.Now()
	_, offset := from.Zone()
	at := now
	if to.Before(now) {
		at = to
	}
	data, err := c.store.StatsData(ctx, StatsQuery{From: from, To: to, At: at, StuckBefore: now.Add(-stuckAfter), OffsetSec: offset})
	if err != nil {
		return st, err
	}
	st.NewUsers, st.Trials, st.FirstPaid, st.ActiveSubs = data.NewUsers, data.Trials, data.FirstPaid, data.ActiveSubs
	st.StuckTasks, st.StuckUsers = data.StuckTasks, data.StuckUsers
	byGateway := map[string]int64{}
	for i := 0; from.AddDate(0, 0, i).Before(to); i++ {
		day := from.AddDate(0, 0, i)
		key := (day.Unix() + int64(offset)) / 86400
		d := DayStat{Day: day}
		for gw, v := range data.Paid[key] {
			d.Gross += v
			byGateway[gw] += v
		}
		for gw, v := range data.Refunded[key] {
			d.Refunded += v
			byGateway[gw] -= v
		}
		d.Revenue = d.Gross - d.Refunded
		st.Revenue += d.Revenue
		st.Days = append(st.Days, d)
	}
	for gw, v := range byGateway {
		st.Gateways = append(st.Gateways, GatewayRevenue{Gateway: gw, Revenue: v})
	}
	sort.Slice(st.Gateways, func(i, j int) bool { return st.Gateways[i].Gateway < st.Gateways[j].Gateway })
	return st, nil
}
