package api

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/bini69-oi/worker/core"
)

func (e *adminEnv) payment(tg int64, gateway string, amount int64) core.Payment {
	e.t.Helper()
	u, err := e.c.UserByTg(contextBG, tg)
	if err != nil {
		u, err = e.c.Register(contextBG, tg, fmt.Sprintf("user%d", tg), "ru")
	}
	if err != nil {
		e.t.Fatal(err)
	}
	p, _, err := e.c.CreatePayment(contextBG, u.ID, gateway, amount)
	if err != nil {
		e.t.Fatal(err)
	}
	return p
}

func TestAdminPaymentsListGolden(t *testing.T) {
	e := newAdminEnv(t)
	a := e.payment(3001, "manual", 10000)
	e.payment(3002, "manual", 20000)
	if err := e.c.ConfirmPayment(contextBG, adminOwner, a.ID); err != nil {
		t.Fatal(err)
	}
	rec := e.get(e.owner, "/api/admin/payments?gateway=manual&status=pending")
	if rec.Code != 200 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	golden(t, "admin_payments_list", rec.Body.Bytes())
	if strings.Contains(rec.Body.String(), `"paid"`) {
		t.Errorf("в ждущих оказался оплаченный")
	}
	if rec = e.get(e.owner, "/api/admin/payments?from=вчера"); rec.Code != 400 {
		t.Errorf("плохая дата: %d", rec.Code)
	}
	if rec = e.get(e.owner, "/api/admin/payments?to=2026-01-01"); rec.Code != 200 || !strings.Contains(rec.Body.String(), `"user3001"`) {
		t.Errorf("«по» включает день: %d %s", rec.Code, rec.Body)
	}
	if rec = e.get(e.owner, "/api/admin/payments?status=чужой"); rec.Code != 400 {
		t.Errorf("плохой статус: %d", rec.Code)
	}
}

// Двойной клик «Подтвердить»: деньги один раз, второй клик получает «уже зачислен».
func TestAdminConfirmDoubleClick(t *testing.T) {
	e := newAdminEnv(t)
	p := e.payment(3001, "manual", 10000)
	var wg sync.WaitGroup
	codes := make(chan int, 2)
	for range 2 {
		wg.Go(func() { codes <- e.post(e.owner, fmt.Sprintf("/api/admin/payments/%d/confirm", p.ID), `{}`).Code })
	}
	wg.Wait()
	close(codes)
	ok, dup := 0, 0
	for c := range codes {
		switch c {
		case 200:
			ok++
		case 409:
			dup++
		}
	}
	if ok != 1 || dup != 1 {
		t.Fatalf("200 было %d, 409 было %d", ok, dup)
	}
	u, _ := e.c.UserByTg(contextBG, 3001)
	if u.Balance != 10000 {
		t.Errorf("баланс %d", u.Balance)
	}
	rec := e.post(e.owner, fmt.Sprintf("/api/admin/payments/%d/confirm", p.ID), `{}`)
	if errCode(t, rec) != core.ErrAlreadyPaid.Code {
		t.Errorf("код второго клика: %s", rec.Body)
	}
}

func TestAdminRefundOwnerOnly(t *testing.T) {
	e := newAdminEnv(t)
	e.stars.Refunder = func(context.Context, int64, string) error { return nil }
	p := e.payment(3001, "stars", 30000)
	_, err := e.c.UserByTg(contextBG, 3001)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.c.Credit(contextBG, "stars", core.GatewayEvent{PaymentID: p.ID, ExternalID: "charge-1", ExtAmount: stPrice(t, e, p), Status: core.PayPaid}); err != nil {
		t.Fatal(err)
	}
	helper := e.helper(80, core.PermGrant, core.PermStats)
	path := fmt.Sprintf("/api/admin/payments/%d/refund", p.ID)
	if rec := e.post(helper, path, `{}`); rec.Code != 403 {
		t.Fatalf("помощник вернул платёж: %d %s", rec.Code, rec.Body)
	}
	if u, _ := e.c.UserByTg(contextBG, 3001); u.Balance != 30000 {
		t.Fatalf("баланс после отказа помощнику: %d", u.Balance)
	}
	if rec := e.post(e.owner, path, `{}`); rec.Code != 200 {
		t.Fatalf("владелец: %d %s", rec.Code, rec.Body)
	}
	if u, _ := e.c.UserByTg(contextBG, 3001); u.Balance != 0 {
		t.Errorf("баланс после возврата: %d", u.Balance)
	}
	log := e.st.Audit()
	if log[len(log)-1].Action != "payment.refund" {
		t.Errorf("журнал: %+v", log[len(log)-1])
	}
}

func stPrice(t *testing.T, e *adminEnv, p core.Payment) int64 {
	t.Helper()
	return e.st.Payment(p.ID).ExtAmount
}
