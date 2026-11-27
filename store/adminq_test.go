package store

import (
	"context"
	"testing"
	"time"

	"github.com/bini69-oi/worker/core"
)

// seedUsers заводит абонентов для поиска: имя, код, подписка (nil без неё) и бан.
func seedUsers(t *testing.T, s *CoreStore) map[string]int64 {
	t.Helper()
	ids := map[string]int64{}
	add := func(tg int64, name, ref string, sub *core.Subscription, banned bool) {
		tx(t, s, func(tx core.Tx) error {
			id, err := tx.CreateUser(core.User{TgID: tg, Username: name, Lang: "ru", RefCode: ref, CreatedAt: tNow})
			ids[name] = id
			if err == nil && banned {
				err = tx.SetBanned(id, tNow)
			}
			if err != nil || sub == nil {
				return err
			}
			sub.UserID, sub.CreatedAt, sub.UpdatedAt = id, tNow, tNow
			_, err = tx.SaveSub(*sub)
			return err
		})
	}
	day := 24 * time.Hour
	add(2001, "alice", "REFALICE", &core.Subscription{Status: core.SubActive, PeriodDays: 30, ExpiresAt: tNow.Add(10 * day)}, false)
	add(2002, "a%_x", "REFPCT", nil, false)
	add(2003, "abcde", "REFABC", &core.Subscription{Status: core.SubActive, Trial: true, PeriodDays: 3, ExpiresAt: tNow.Add(day)}, false)
	add(2004, "old", "REFOLD", &core.Subscription{Status: core.SubActive, PeriodDays: 30, ExpiresAt: tNow.Add(-5 * day)}, false)
	add(2005, "ancient", "REFANC", &core.Subscription{Status: core.SubActive, PeriodDays: 30, ExpiresAt: tNow.Add(-60 * day)}, false)
	add(2006, "bad", "REFBAD", &core.Subscription{Status: core.SubActive, PeriodDays: 30, ExpiresAt: tNow.Add(10 * day)}, true)
	return ids
}

func names(rows []core.UserRow) []string {
	var out []string
	for _, r := range rows {
		out = append(out, r.User.Username)
	}
	return out
}

func eq(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestFindUsers(t *testing.T) {
	s, _ := newCoreStore(t) // в базе уже есть ivan (1001) без кода рефералки
	seedUsers(t, s)
	ctx := context.Background()
	cases := []struct {
		name string
		q    core.UserQuery
		want []string
	}{
		{"по Telegram ID", core.UserQuery{TgID: 2001}, []string{"alice"}},
		{"по коду рефералки", core.UserQuery{RefCode: "REFABC"}, []string{"abcde"}},
		{"начало имени без учёта регистра", core.UserQuery{UsernamePrefix: "AL"}, []string{"alice"}},
		{"спецсимволы как есть", core.UserQuery{UsernamePrefix: "a%_"}, []string{"a%_x"}},
		{"процент это не маска", core.UserQuery{UsernamePrefix: "%"}, nil},
		{"подчёркивание это не маска", core.UserQuery{UsernamePrefix: "_"}, nil},
		{"обратная косая", core.UserQuery{UsernamePrefix: `\`}, nil},
		{"все с подпиской", core.UserQuery{Filter: core.FilterActive}, []string{"bad", "abcde", "alice"}},
		{"пробный", core.UserQuery{Filter: core.FilterTrial}, []string{"abcde"}},
		{"истекла", core.UserQuery{Filter: core.FilterExpired}, []string{"ancient", "old"}},
		{"без подписки", core.UserQuery{Filter: core.FilterNone}, []string{"a%_x", "ivan"}},
		{"забанен", core.UserQuery{Filter: core.FilterBanned}, []string{"bad"}},
		{"истекла за 30 дней", core.UserQuery{Filter: core.SegExpired30}, []string{"old"}},
		{"id и фильтр вместе", core.UserQuery{TgID: 2004, Filter: core.FilterActive}, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rows, err := s.FindUsers(ctx, c.q, tNow, 50)
			if err != nil {
				t.Fatal(err)
			}
			if got := names(rows); !eq(got, c.want) {
				t.Errorf("нашли %v, ждали %v", got, c.want)
			}
		})
	}
}

func TestFindUsersCursor(t *testing.T) {
	s, _ := newCoreStore(t)
	seedUsers(t, s)
	ctx := context.Background()
	first, err := s.FindUsers(ctx, core.UserQuery{}, tNow, 3)
	if err != nil || len(first) != 3 {
		t.Fatalf("первая страница: %v %v", first, err)
	}
	second, err := s.FindUsers(ctx, core.UserQuery{After: first[2].User.ID}, tNow, 3)
	if err != nil || len(second) != 3 {
		t.Fatalf("вторая страница: %v %v", second, err)
	}
	if first[2].User.ID <= second[0].User.ID {
		t.Errorf("страницы пересеклись или идут не по убыванию: %d, %d", first[2].User.ID, second[0].User.ID)
	}
}

func TestFindUsersRowFields(t *testing.T) {
	s, _ := newCoreStore(t)
	seedUsers(t, s)
	rows, err := s.FindUsers(context.Background(), core.UserQuery{TgID: 2003}, tNow, 5)
	if err != nil || len(rows) != 1 {
		t.Fatal(rows, err)
	}
	r := rows[0]
	if !r.HasSub || !r.Trial || r.SubStatus != core.SubActive || r.Class(tNow) != core.FilterTrial {
		t.Errorf("подписка в строке: %+v", r)
	}
}

func TestAuditQuery(t *testing.T) {
	s, _ := newCoreStore(t)
	ctx := context.Background()
	at := func(h int) time.Time { return tNow.Add(time.Duration(h) * time.Hour) }
	tx(t, s, func(tx core.Tx) error {
		for i, e := range []core.AuditEntry{
			{AdminTg: 1, Action: "sub.grant_days", Target: "5", Details: map[string]string{"days": "30"}, At: at(0)},
			{AdminTg: 2, Action: "user.ban", Target: "5", At: at(1)},
			{AdminTg: 1, Action: "sub.grant_days", Target: "6", At: at(2)},
		} {
			if err := tx.AddAudit(e); err != nil {
				t.Fatal(i, err)
			}
		}
		return nil
	})
	rows, err := s.AuditLog(ctx, core.AuditFilter{Action: "sub.grant_days"}, 50)
	if err != nil || len(rows) != 2 || rows[0].Target != "6" || rows[1].Details["days"] != "30" {
		t.Fatalf("по действию: %+v %v", rows, err)
	}
	if rows, _ = s.AuditLog(ctx, core.AuditFilter{Target: "5"}, 50); len(rows) != 2 {
		t.Errorf("по цели: %d", len(rows))
	}
	if rows, _ = s.AuditLog(ctx, core.AuditFilter{AdminTg: 2}, 50); len(rows) != 1 || rows[0].Action != "user.ban" {
		t.Errorf("по админу: %+v", rows)
	}
	if rows, _ = s.AuditLog(ctx, core.AuditFilter{From: at(1), To: at(2)}, 50); len(rows) != 1 {
		t.Errorf("по датам: %+v", rows)
	}
	all, _ := s.AuditLog(ctx, core.AuditFilter{}, 50)
	if rows, _ = s.AuditLog(ctx, core.AuditFilter{After: all[0].ID}, 50); len(rows) != 2 {
		t.Errorf("курсор: %d", len(rows))
	}
}
