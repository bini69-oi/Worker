package main

import (
	"testing"

	"github.com/bini69-oi/worker/core"
)

// Куда касса вернёт абонента: платёж из браузера на страницу кабинета, остальные в бота.
func TestPaidURLBySource(t *testing.T) {
	tg := newTelegram("123:x", "http://tg.invalid", "https://w.example.com", func() bool { return false }, nil)
	tg.name.Store("workerbot")
	src := map[int64]string{7: core.PaySourceWeb, 8: ""}
	tg.source = func(id int64) string { return src[id] }
	if got := tg.paidURL(7); got != "https://w.example.com/app/#/pay/7" {
		t.Errorf("из браузера: %q", got)
	}
	if got := tg.paidURL(8); got != "https://t.me/workerbot?start=paid_8" {
		t.Errorf("из Mini App или бота: %q", got)
	}
	if got := tg.paidURL(9); got != "https://t.me/workerbot?start=paid_9" {
		t.Errorf("неизвестный платёж: %q", got)
	}
}
