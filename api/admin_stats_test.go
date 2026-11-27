package api

import (
	"encoding/json"
	"testing"

	"github.com/bini69-oi/worker/core"
)

func TestAdminStatsGolden(t *testing.T) {
	e := newAdminEnv(t)
	// Сегодня 1 января 12:00: оплата 200 ₽ ручным переводом и пробный абонент.
	e.subscriber(2001, "ivan")
	p := e.payment(2002, "manual", 100000)
	if err := e.c.ConfirmPayment(contextBG, adminOwner, p.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.c.Trial(contextBG, mustUser(t, e, 2002).ID); err != nil {
		t.Fatal(err)
	}
	rec := e.get(e.owner, "/api/admin/stats")
	if rec.Code != 200 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	golden(t, "admin_stats", rec.Body.Bytes())
	var st AdminStats
	if err := json.Unmarshal(rec.Body.Bytes(), &st); err != nil {
		t.Fatal(err)
	}
	if st.Revenue != 100000 || st.NewUsers != 2 || st.Trials != 1 || st.FirstPaid != 1 || st.ActiveSubs != 2 || len(st.Days) != 1 || st.From != "2026-01-01" {
		t.Errorf("%+v", st)
	}
}

func mustUser(t *testing.T, e *adminEnv, tg int64) core.User {
	t.Helper()
	u, err := e.c.UserByTg(contextBG, tg)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func TestAdminStatsPeriod(t *testing.T) {
	e := newAdminEnv(t)
	rec := e.get(e.owner, "/api/admin/stats?from=2025-12-01&to=2026-01-31")
	var st AdminStats
	if err := json.Unmarshal(rec.Body.Bytes(), &st); err != nil || rec.Code != 200 || len(st.Days) != 62 {
		t.Fatalf("%d дней: %d %s", len(st.Days), rec.Code, rec.Body)
	}
	rec = e.get(e.owner, "/api/admin/stats?from=2026-01-01&to=2026-07-20")
	if rec.Code != 400 || errCode(t, rec) != core.ErrPeriodTooLong.Code {
		t.Errorf("200 дней: %d %s", rec.Code, rec.Body)
	}
	for _, q := range []string{"from=вчера", "to=завтра", "from=2026-02-01&to=2026-01-01"} {
		if rec = e.get(e.owner, "/api/admin/stats?"+q); rec.Code != 400 {
			t.Errorf("%s: %d", q, rec.Code)
		}
	}
	h := e.helper(90, core.PermStats)
	if rec = e.get(h, "/api/admin/stats"); rec.Code != 200 {
		t.Errorf("помощник со stats: %d", rec.Code)
	}
}
