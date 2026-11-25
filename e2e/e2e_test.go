// Package e2e это сквозные проверки ядра на настоящих частях: SQLite, HTTP-клиент панели с фейковой
// панелью 3.x, касса с вебхуком через общий обработчик /pay/{name}.
package e2e

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/bini69-oi/worker/core"
	"github.com/bini69-oi/worker/core/coretest"
	"github.com/bini69-oi/worker/panel"
	"github.com/bini69-oi/worker/panel/paneltest"
	"github.com/bini69-oi/worker/pay"
	"github.com/bini69-oi/worker/pay/gatewaytest"
	"github.com/bini69-oi/worker/settings"
	"github.com/bini69-oi/worker/store"
)

const owner = 1

type world struct {
	c      *core.Core
	db     *store.DB
	s      *store.CoreStore
	cfg    *settings.Settings
	clock  *coretest.Clock
	panel  *paneltest.Panel
	kassa  *gatewaytest.Fake
	notes  *coretest.Notifier
	payAPI http.Handler
}

func newWorld(t *testing.T) *world {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "worker.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	cfg, err := settings.New(store.NewSettingsStore(db), settings.NewKey(), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.Set("fake.enabled", "true"); err != nil {
		t.Fatal(err)
	}
	w := &world{db: db, s: store.NewCoreStore(db), cfg: cfg, notes: &coretest.Notifier{},
		clock: coretest.NewClock(time.Now().UTC().Truncate(time.Second)),
		panel: paneltest.New(t, paneltest.V3), kassa: &gatewaytest.Fake{Secret: "kassa-secret"}}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	w.c = core.New(core.Deps{
		Store: w.s, Clock: w.clock, Config: cfg, Notifier: w.notes, OwnerID: owner, Log: log,
		Panel:    panel.CoreAdapter{Client: panel.New(w.panel.URL(), paneltest.Token, nil)},
		Gateways: []core.Gateway{w.kassa},
	})
	w.payAPI = pay.Handler(w.c, []pay.Gateway{w.kassa}, log)
	return w
}

func (w *world) process(t *testing.T) {
	t.Helper()
	if _, err := w.c.ProcessDue(context.Background()); err != nil {
		t.Fatal(err)
	}
}

// pay проходит оплату как абонент: платёж, вебхук кассы через общий обработчик.
func (w *world) pay(t *testing.T, userID, amount int64) {
	t.Helper()
	p, _, err := w.c.CreatePayment(context.Background(), userID, "fake", amount)
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	w.payAPI.ServeHTTP(rec, w.kassa.Hook(p, amount, false))
	if rec.Code != http.StatusOK {
		t.Fatalf("вебхук кассы: код %d", rec.Code)
	}
}

func (w *world) panelUser(t *testing.T) paneltest.User {
	t.Helper()
	users := w.panel.Users()
	if len(users) != 1 {
		t.Fatalf("в панели абонентов %d, ждали 1", len(users))
	}
	return users[0]
}

// TestSubscriberJourney: новый абонент, пробный период, пополнение, покупка, продление, окончание.
func TestSubscriberJourney(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	tr, err := w.c.SaveTariff(ctx, 1, core.Tariff{Name: "Месяц", Active: true, Devices: 3, TrafficBytes: 100 << 30,
		Squads: []string{"11111111-1111-1111-1111-111111111111"}, Periods: []core.Period{{Days: 30, Price: 15000}}})
	if err != nil {
		t.Fatal(err)
	}

	u, err := w.c.Register(ctx, 777000, "ivan", "ru")
	if err != nil {
		t.Fatal(err)
	}
	trial, err := w.c.Trial(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	w.process(t)
	if pu := w.panelUser(t); pu.Status != "ACTIVE" || !pu.ExpireAt.Equal(trial.ExpiresAt) || pu.Username != "u777000" {
		t.Fatalf("пробный период в панели: %+v", pu)
	}

	w.pay(t, u.ID, 20000)
	sub, err := w.c.Buy(ctx, u.ID, tr.ID, 30)
	if err != nil {
		t.Fatal(err)
	}
	w.process(t)
	if pu := w.panelUser(t); !pu.ExpireAt.Equal(sub.ExpiresAt) || pu.DeviceLimit != 3 || pu.TrafficMax != 100<<30 {
		t.Fatalf("покупка в панели: %+v", pu)
	}
	saved, _ := w.c.Sub(ctx, u.ID)
	link := saved.SubURL

	w.clock.Add(25 * 24 * time.Hour)
	w.pay(t, u.ID, 10000)
	renewed, err := w.c.Buy(ctx, u.ID, tr.ID, 30)
	if err != nil {
		t.Fatal(err)
	}
	w.process(t)
	if !renewed.ExpiresAt.Equal(sub.ExpiresAt.Add(30 * 24 * time.Hour)) {
		t.Fatalf("продление не от даты окончания: %v", renewed.ExpiresAt)
	}
	if after, _ := w.c.Sub(ctx, u.ID); after.SubURL != link {
		t.Fatal("ссылка поменялась при продлении")
	}

	w.clock.Add(70 * 24 * time.Hour)
	if n, err := w.c.ExpireDue(ctx); err != nil || n != 1 {
		t.Fatalf("окончание: %d (%v)", n, err)
	}
	w.process(t)
	if pu := w.panelUser(t); pu.Status != "DISABLED" {
		t.Fatalf("после окончания абонент в панели включён: %+v", pu)
	}

	hist, _ := w.c.History(ctx, u.ID, 10)
	got, _ := w.s.User(ctx, u.ID)
	var sum int64
	for _, e := range hist {
		sum += e.Amount
	}
	if got.Balance != 0 || sum != 0 || len(hist) != 4 {
		t.Fatalf("деньги не сходятся: баланс %d, сумма истории %d, строк %d", got.Balance, sum, len(hist))
	}
}
