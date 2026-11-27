package api

import (
	"net/http"
	"time"

	"github.com/bini69-oi/worker/core"
)

// AdminDay это выручка одного дня за вычетом возвратов дня, копейки.
type AdminDay struct {
	Day      string `json:"day"` // 2026-01-31
	Gross    int64  `json:"gross"`
	Refunded int64  `json:"refunded"`
	Revenue  int64  `json:"revenue"`
}

// AdminGatewayRevenue это выручка кассы за период.
type AdminGatewayRevenue struct {
	Gateway string `json:"gateway"`
	Revenue int64  `json:"revenue"`
}

// AdminStats это сводка за период. StuckUsers это абоненты, у которых зависла очередь панели: ссылки на карточки.
type AdminStats struct {
	From       string                `json:"from"`
	To         string                `json:"to"`
	Days       []AdminDay            `json:"days"`
	Gateways   []AdminGatewayRevenue `json:"gateways"`
	Revenue    int64                 `json:"revenue"`
	NewUsers   int                   `json:"new_users"`
	Trials     int                   `json:"trials"`
	FirstPaid  int                   `json:"first_paid"`
	ActiveSubs int                   `json:"active_subs"`
	StuckTasks int                   `json:"stuck_tasks"`
	StuckUsers []int64               `json:"stuck_users"`
}

const dayLayout = "2006-01-02"

func (a *adminAPI) loc() *time.Location {
	if a.d.Loc != nil {
		return a.d.Loc
	}
	return time.Local
}

func (a *adminAPI) statsGet(_ http.ResponseWriter, r *http.Request, ad adminCtx) (any, error) {
	loc := a.loc()
	q := r.URL.Query()
	now := time.Now().In(loc)
	if a.s.deps.Now != nil {
		now = a.s.deps.Now().In(loc)
	}
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	from, to := today, today
	var err error
	if raw := q.Get("from"); raw != "" {
		if from, err = time.ParseInLocation(dayLayout, raw, loc); err != nil {
			return nil, core.ErrInvalid
		}
	}
	if raw := q.Get("to"); raw != "" {
		if to, err = time.ParseInLocation(dayLayout, raw, loc); err != nil {
			return nil, core.ErrInvalid
		}
	}
	st, err := a.d.Core.Stats(r.Context(), ad.Admin.TgID, from, to.AddDate(0, 0, 1)) // «по» включает весь день
	if err != nil {
		return nil, err
	}
	out := AdminStats{From: from.Format(dayLayout), To: to.Format(dayLayout), Days: make([]AdminDay, 0, len(st.Days)),
		Gateways: make([]AdminGatewayRevenue, 0, len(st.Gateways)), Revenue: st.Revenue, NewUsers: st.NewUsers, Trials: st.Trials,
		FirstPaid: st.FirstPaid, ActiveSubs: st.ActiveSubs, StuckTasks: st.StuckTasks, StuckUsers: append([]int64{}, st.StuckUsers...)}
	for _, d := range st.Days {
		out.Days = append(out.Days, AdminDay{Day: d.Day.Format(dayLayout), Gross: d.Gross, Refunded: d.Refunded, Revenue: d.Revenue})
	}
	for _, g := range st.Gateways {
		out.Gateways = append(out.Gateways, AdminGatewayRevenue{Gateway: g.Gateway, Revenue: g.Revenue})
	}
	return out, nil
}
