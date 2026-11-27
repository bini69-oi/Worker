package core_test

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/bini69-oi/worker/core"
	"github.com/bini69-oi/worker/core/coretest"
	"github.com/bini69-oi/worker/redact"
)

func TestAuditFilterByAction(t *testing.T) {
	ctx := context.Background()
	e := newCfgEnv(t)
	e.bought(t)
	if _, err := e.c.GrantDays(ctx, owner, e.user.ID, 30); err != nil {
		t.Fatal(err)
	}
	if err := e.c.Ban(ctx, owner, e.user.ID, "тест"); err != nil {
		t.Fatal(err)
	}
	page, err := e.c.Audit(ctx, owner, core.AuditFilter{Action: "sub.grant_days"}, 0)
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("%+v %v", page, err)
	}
	r := page.Items[0]
	if r.AdminTg != owner || r.Target != strconv.FormatInt(e.user.ID, 10) || r.Details["days"] != "30" || r.At.IsZero() {
		t.Errorf("кто, кому, сколько и когда: %+v", r)
	}
	if page, _ = e.c.Audit(ctx, owner, core.AuditFilter{AdminTg: 999}, 0); len(page.Items) != 0 {
		t.Errorf("чужой админ: %+v", page.Items)
	}
	e.clock.Add(48 * time.Hour)
	if page, _ = e.c.Audit(ctx, owner, core.AuditFilter{From: e.clock.Now().Add(-time.Hour)}, 0); len(page.Items) != 0 {
		t.Errorf("по датам: %+v", page.Items)
	}
}

func TestAuditPages(t *testing.T) {
	ctx := context.Background()
	e := newCfgEnv(t)
	for i := range 120 {
		if _, err := e.c.AdminAdjust(ctx, owner, e.user.ID, 100, "п"+strconv.Itoa(i)); err != nil {
			t.Fatal(err)
		}
	}
	f := core.AuditFilter{Action: "balance.adjust"}
	first, _ := e.c.Audit(ctx, owner, f, 0)
	second, _ := e.c.Audit(ctx, owner, f, first.Next)
	third, _ := e.c.Audit(ctx, owner, f, second.Next)
	if len(first.Items) != 50 || len(second.Items) != 50 || len(third.Items) != 20 || third.Next != 0 {
		t.Fatalf("страницы: %d %d %d", len(first.Items), len(second.Items), len(third.Items))
	}
	if first.Items[49].ID <= second.Items[0].ID {
		t.Errorf("страницы пересеклись")
	}
}

// Смена ключа в журнале: видно, какой ключ сменён, значения ключа нет.
func TestAuditHasNoSecret(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	set := &redact.Set{}
	tx := coretest.NewTexts(t)
	c := core.New(core.Deps{Store: e.st, Clock: e.clock, Config: e.cfg, Panel: e.panel, Notifier: e.notes, OwnerID: owner, Texts: tx, Secrets: set})
	const secret = "live_0123456789abcdefXYZ1"
	res, err := c.SetSetting(ctx, owner, "yookassa.secret_key", secret)
	if err != nil || !res.Pending {
		t.Fatal(res, err)
	}
	if err := c.AnswerConfirm(ctx, res.ConfirmID, owner, true); err != nil {
		t.Fatal(err)
	}
	page, err := c.Audit(ctx, owner, core.AuditFilter{}, 0)
	if err != nil {
		t.Fatal(err)
	}
	keyShown := false
	for _, r := range page.Items {
		for _, v := range r.Details {
			if strings.Contains(v, secret) {
				t.Fatalf("секрет в журнале: %+v", r)
			}
			keyShown = keyShown || strings.Contains(v, "yookassa.secret_key")
		}
	}
	if !keyShown {
		t.Errorf("в журнале не видно, какой ключ сменён: %+v", page.Items)
	}
	// Ошибка автора: секрет попал в подробности записи. Выдача журнала его всё равно вычистит.
	set.Add(secret)
	if err := c.SetText(ctx, owner, "buy.success", "ru", "ключ "+secret+" {days}"); err != nil {
		t.Fatal(err)
	}
	if err := e.st.Tx(ctx, func(tx core.Tx) error {
		return tx.AddAudit(core.AuditEntry{AdminTg: owner, Action: "test.leak", Details: map[string]string{"v": "до " + secret}, At: e.clock.Now()})
	}); err != nil {
		t.Fatal(err)
	}
	page, _ = c.Audit(ctx, owner, core.AuditFilter{Action: "test.leak"}, 0)
	if len(page.Items) != 1 || strings.Contains(page.Items[0].Details["v"], secret) {
		t.Errorf("фильтр секретов не сработал: %+v", page.Items)
	}
}

func TestAuditOwnerOnly(t *testing.T) {
	ctx := context.Background()
	e := newCfgEnv(t)
	helperWith(t, e.env, 50, core.PermUsers, core.PermStats)
	if _, err := e.c.Audit(ctx, 50, core.AuditFilter{}, 0); !errors.Is(err, core.ErrOwnerOnly) {
		t.Errorf("помощник: %v", err)
	}
}
