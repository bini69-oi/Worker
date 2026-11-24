package settings

import (
	"context"
	"testing"
	"time"
)

// fakeClock даёт тикер, которым управляет тест, и время, которое идёт только по его команде.
type fakeClock struct {
	now  time.Time
	tick chan time.Time
}

func (c *fakeClock) advance(d time.Duration) {
	c.now = c.now.Add(d)
	c.tick <- c.now
}

// Правка другим процессом (прямо в базе) видна после ближайшего сигнала, то есть не позже 3 секунд.
func TestChangeVisibleWithin5Seconds(t *testing.T) {
	f := newFake()
	clock := &fakeClock{now: t0, tick: make(chan time.Time)}
	s, err := New(f, func() time.Time { return clock.now })
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { s.Run(ctx, clock.tick); close(done) }()

	if !s.Bool("trial.enabled") {
		t.Fatal("пробный период по умолчанию включён")
	}
	f.Save("trial.enabled", "false", clock.now.Unix()) // админ выключил, минуя наш кэш

	clock.advance(RefreshEvery)
	// Сигнал принят: второй сигнал примет цикл только после того, как закончит перечитывание.
	clock.advance(RefreshEvery)
	if s.Bool("trial.enabled") {
		t.Fatal("через 3 секунды правка должна быть видна")
	}
	if RefreshEvery >= 5*time.Second {
		t.Fatal("период перечитывания должен быть меньше 5 секунд")
	}

	// Сброс (удаление строки) тоже подхватывается.
	f.Delete("trial.enabled")
	clock.advance(RefreshEvery)
	clock.advance(RefreshEvery)
	if !s.Bool("trial.enabled") {
		t.Fatal("после удаления строки должно вернуться умолчание")
	}

	cancel()
	<-done
}

func TestRunSurvivesStoreFail(t *testing.T) {
	f := newFake()
	clock := &fakeClock{now: t0, tick: make(chan time.Time)}
	s, err := New(f, func() time.Time { return clock.now })
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Set("trial.days", "7"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { s.Run(ctx, clock.tick); close(done) }()

	f.fail = errFake
	clock.advance(RefreshEvery)
	clock.advance(RefreshEvery)
	if s.Int("trial.days") != 7 {
		t.Fatal("при сбое чтения должен остаться прежний кэш")
	}
	cancel()
	<-done
}
