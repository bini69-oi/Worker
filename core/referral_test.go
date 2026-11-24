package core_test

import (
	"context"
	"testing"

	"github.com/bini69-oi/worker/core"
)

func TestReferralLink(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	if len(e.user.RefCode) != 8 {
		t.Fatalf("у абонента нет личного кода: %q", e.user.RefCode)
	}
	friend, err := e.c.RegisterRef(ctx, 2002, "petr", "ru", e.user.RefCode)
	if err != nil || friend.ReferrerID != e.user.ID {
		t.Fatalf("новый абонент по ссылке: пригласивший %d (%v)", friend.ReferrerID, err)
	}
	stranger, _ := e.c.Register(ctx, 3003, "anna", "ru")
	again, _ := e.c.RegisterRef(ctx, 3003, "anna", "ru", e.user.RefCode)
	if again.ReferrerID != 0 || again.ID != stranger.ID {
		t.Fatalf("старый абонент по чужой ссылке сменил пригласившего: %+v", again)
	}
	self, _ := e.c.RegisterRef(ctx, 1001, "ivan", "ru", e.user.RefCode)
	if self.ReferrerID != 0 {
		t.Fatal("абонент записан пригласившим самого себя")
	}
	if friend.RefCode == e.user.RefCode {
		t.Fatal("у двух абонентов один код")
	}
}

func TestReferralFirstPurchasePercent(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	tr := e.tariff(t, "Месяц", core.Period{Days: 30, Price: 15000})
	friend, _ := e.c.RegisterRef(ctx, 2002, "petr", "ru", e.user.RefCode)
	if _, err := e.c.AdminAdjust(ctx, 1, friend.ID, 30000, "тест"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.c.Buy(ctx, friend.ID, tr.ID, 30); err != nil {
		t.Fatal(err)
	}
	if b := balance(t, e.st, e.user.ID); b != 1500 {
		t.Fatalf("10%% от 15000: ждали 1500, получили %d", b)
	}
	if _, err := e.c.Buy(ctx, friend.ID, tr.ID, 30); err != nil {
		t.Fatal(err)
	}
	if b := balance(t, e.st, e.user.ID); b != 1500 {
		t.Fatalf("без процента с каждой покупки вторая покупка дала бонус: %d", b)
	}
}

func TestReferralEachPurchase(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	if err := e.cfg.Set("ref.each_percent", "5"); err != nil {
		t.Fatal(err)
	}
	tr := e.tariff(t, "Месяц", core.Period{Days: 30, Price: 15000})
	friend, _ := e.c.RegisterRef(ctx, 2002, "petr", "ru", e.user.RefCode)
	if _, err := e.c.AdminAdjust(ctx, 1, friend.ID, 30000, "тест"); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if _, err := e.c.Buy(ctx, friend.ID, tr.ID, 30); err != nil {
			t.Fatal(err)
		}
	}
	if b := balance(t, e.st, e.user.ID); b != 1500+750 {
		t.Fatalf("первая 10%% и вторая 5%%: ждали 2250, получили %d", b)
	}
}

func TestReferralWelcomeBonusAndStats(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	if err := e.cfg.Set("ref.welcome_bonus", "5000"); err != nil {
		t.Fatal(err)
	}
	tr := e.tariff(t, "Месяц", core.Period{Days: 30, Price: 5000})
	var friends []core.User
	for i := range 3 {
		f, err := e.c.RegisterRef(ctx, int64(2000+i), "f", "ru", e.user.RefCode)
		if err != nil {
			t.Fatal(err)
		}
		if f.Balance != 5000 {
			t.Fatalf("приглашённый не получил бонус при входе: %d", f.Balance)
		}
		friends = append(friends, f)
	}
	for _, f := range friends[:2] {
		if _, err := e.c.Buy(ctx, f.ID, tr.ID, 30); err != nil {
			t.Fatal(err)
		}
	}
	st, err := e.c.RefStats(ctx, e.user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if st.Invited != 3 || st.Bought != 2 || st.Earned != 1000 {
		t.Fatalf("ждали приглашено 3, купили 2, заработано 1000: %+v", st)
	}
}
