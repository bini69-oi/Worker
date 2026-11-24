// Package gatewaytest это общий набор тестов, который проходит каждая касса:
// поддельная подпись, повтор вебхука, неверная сумма, касса без ключей.
package gatewaytest

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/bini69-oi/worker/core"
	"github.com/bini69-oi/worker/core/coretest"
	"github.com/bini69-oi/worker/pay"
)

// Case описывает кассу для общего набора.
type Case struct {
	// Gateway это касса с заполненными ключами. Если она ходит в API кассы, то в фейковый httptest-сервер.
	Gateway pay.Gateway
	// Unconfigured это та же касса без ключей.
	Unconfigured pay.Gateway
	// Paid собирает подписанный вебхук «платёж p оплачен на extAmount». nil, если касса не принимает вебхуки.
	Paid func(p core.Payment, extAmount int64) *http.Request
	// Forged собирает тот же вебхук с испорченной подписью.
	Forged func(p core.Payment, extAmount int64) *http.Request
	// Amount это сумма пополнения в копейках, которая проходит пределы кассы.
	Amount int64
}

type env struct {
	c    *core.Core
	st   *coretest.Store
	user core.User
	h    http.Handler
}

func newEnv(t *testing.T, g pay.Gateway) *env {
	t.Helper()
	cfg := coretest.NewConfig(t)
	if err := cfg.Set(g.Name()+".enabled", "true"); err != nil {
		t.Fatalf("у кассы %s нет общих настроек (gatewayDefs в settings): %v", g.Name(), err)
	}
	st := coretest.NewStore()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	c := core.New(core.Deps{Store: st, Clock: coretest.NewClock(time.Now()), Config: cfg,
		Gateways: []core.Gateway{g}, Log: log})
	u, err := c.Register(context.Background(), 1001, "ivan", "ru")
	if err != nil {
		t.Fatal(err)
	}
	return &env{c: c, st: st, user: u, h: pay.Handler(c, []pay.Gateway{g}, log)}
}

func (e *env) post(r *http.Request) int {
	w := httptest.NewRecorder()
	e.h.ServeHTTP(w, r)
	return w.Code
}

func (e *env) balance(t *testing.T) int64 {
	t.Helper()
	u, err := e.st.User(context.Background(), e.user.ID)
	if err != nil {
		t.Fatal(err)
	}
	return u.Balance
}

// Run прогоняет общий набор.
func Run(t *testing.T, tc Case) {
	t.Run("касса без ключей не видна", func(t *testing.T) {
		if tc.Unconfigured.Configured() {
			t.Fatal("касса без ключей говорит, что настроена")
		}
		e := newEnv(t, tc.Unconfigured)
		if len(e.c.Gateways()) != 0 {
			t.Fatal("касса без ключей видна абоненту")
		}
	})

	t.Run("создание платежа", func(t *testing.T) {
		e := newEnv(t, tc.Gateway)
		p, inv, err := e.c.CreatePayment(context.Background(), e.user.ID, tc.Gateway.Name(), tc.Amount)
		if err != nil {
			t.Fatal(err)
		}
		if p.Status != core.PayPending || inv.ExtAmount <= 0 || inv.ExtCurrency == "" {
			t.Fatalf("платёж или счёт не тот: %+v %+v", p, inv)
		}
	})

	if tc.Paid == nil {
		return
	}

	t.Run("поддельная подпись", func(t *testing.T) {
		e := newEnv(t, tc.Gateway)
		p, _, err := e.c.CreatePayment(context.Background(), e.user.ID, tc.Gateway.Name(), tc.Amount)
		if err != nil {
			t.Fatal(err)
		}
		if code := e.post(tc.Forged(e.st.Payment(p.ID), e.st.Payment(p.ID).ExtAmount)); code != http.StatusUnauthorized {
			t.Fatalf("поддельный вебхук: код %d", code)
		}
		if e.balance(t) != 0 {
			t.Fatal("поддельный вебхук зачислил деньги")
		}
	})

	t.Run("вебхук дважды", func(t *testing.T) {
		e := newEnv(t, tc.Gateway)
		p, _, err := e.c.CreatePayment(context.Background(), e.user.ID, tc.Gateway.Name(), tc.Amount)
		if err != nil {
			t.Fatal(err)
		}
		saved := e.st.Payment(p.ID)
		for range 2 {
			if code := e.post(tc.Paid(saved, saved.ExtAmount)); code != http.StatusOK {
				t.Fatalf("вебхук: код %d", code)
			}
		}
		if b := e.balance(t); b != tc.Amount {
			t.Fatalf("баланс %d, ждали %d ровно один раз", b, tc.Amount)
		}
	})

	t.Run("неверная сумма", func(t *testing.T) {
		e := newEnv(t, tc.Gateway)
		p, _, err := e.c.CreatePayment(context.Background(), e.user.ID, tc.Gateway.Name(), tc.Amount)
		if err != nil {
			t.Fatal(err)
		}
		saved := e.st.Payment(p.ID)
		e.post(tc.Paid(saved, saved.ExtAmount-1))
		if e.balance(t) != 0 {
			t.Fatal("оплата на меньшую сумму зачислена")
		}
	})
}
