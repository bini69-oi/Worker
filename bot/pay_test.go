package bot

import (
	"context"
	"strconv"
	"strings"
	"testing"

	"github.com/bini69-oi/worker/core"
	"github.com/bini69-oi/worker/tg"
)

func payEnv(t *testing.T) *env {
	e := newEnv(t)
	e.set(t, "stars.enabled", "true", "fake.enabled", "true", "fake.title", "Карта")
	e.start(t)
	return e
}

func TestTopupScreens(t *testing.T) {
	e := payEnv(t)
	e.press(t, userTg, 5, "tp")
	golden(t, "topup_amounts", e.api.take())
	e.press(t, userTg, 5, "tp:25000")
	golden(t, "topup_gateways", e.api.take())

	// Касса без ключей не видна.
	e.fake.Secret = ""
	e.press(t, userTg, 5, "tp:25000")
	if strings.Contains(render(e.api.take()), "Карта") {
		t.Fatal("касса без ключей видна")
	}
}

func TestOwnAmount(t *testing.T) {
	e := payEnv(t)
	for _, bad := range []string{"1e9", "-5", "350.50", "0", " 3 5", "99999999"} {
		e.press(t, userTg, 5, "tpown")
		e.api.take()
		e.text(t, userTg, bad)
		if got := render(e.api.take()); !strings.Contains(got, "целым числом") {
			t.Fatalf("%q принято: %s", bad, got)
		}
	}
	e.press(t, userTg, 5, "tpown")
	e.text(t, userTg, "350")
	if got := render(e.api.take()); !strings.Contains(got, "Пополнение на <b>350 ₽</b>") {
		t.Fatalf("своя сумма: %s", got)
	}
}

func TestStarsTopup(t *testing.T) {
	e := payEnv(t)
	e.press(t, userTg, 5, "tpg:stars:30000")
	calls := e.api.take()
	if calls[0].method != "invoice" || !strings.Contains(calls[0].text, "XTR p:") {
		t.Fatalf("счёт Stars: %s", render(calls))
	}
	payload := strings.Fields(calls[0].text)[1]
	if got := e.precheckout(t, userTg, "XTR", payload, 999); !strings.HasPrefix(got, "ok=false") {
		t.Fatalf("сумма не совпала, а оплата разрешена: %s", got)
	}
	if got := e.precheckout(t, 2002, "XTR", payload, 200); !strings.HasPrefix(got, "ok=false") {
		t.Fatalf("чужой платёж разрешён: %s", got)
	}
	if got := e.precheckout(t, userTg, "XTR", payload, 200); got != "ok=true " {
		t.Fatalf("верная оплата не разрешена: %s", got)
	}
	sp := &tg.SuccessfulPayment{Currency: "XTR", TotalAmount: 200, InvoicePayload: payload, TelegramPaymentChargeID: "ch-1"}
	e.paid(t, userTg, sp, nil)
	e.paid(t, userTg, sp, nil) // повтор сообщения
	if b := e.balance(t); b != 30000 {
		t.Fatalf("после оплаты Stars баланс %d, ждали 30000", b)
	}

	e.paid(t, userTg, nil, &tg.RefundedPayment{Currency: "XTR", TotalAmount: 200, InvoicePayload: payload, TelegramPaymentChargeID: "ch-1"})
	if b := e.balance(t); b != 0 {
		t.Fatalf("после возврата баланс %d, ждали 0", b)
	}
}

func TestWrongCurrencyNotCredited(t *testing.T) {
	e := payEnv(t)
	e.press(t, userTg, 5, "tpg:stars:30000")
	payload := strings.Fields(e.api.take()[0].text)[1]
	e.paid(t, userTg, &tg.SuccessfulPayment{Currency: "USD", TotalAmount: 200, InvoicePayload: payload, TelegramPaymentChargeID: "ch-2"}, nil)
	if b := e.balance(t); b != 0 {
		t.Fatalf("оплата не в звёздах зачислена: %d", b)
	}
}

func TestStarsSubscription(t *testing.T) {
	e := payEnv(t)
	tr := e.tariff(t, "Месяц", core.Period{Days: 30, Price: 30000})
	id := strconv.FormatInt(tr.ID, 10)
	e.press(t, userTg, 5, "bt:"+id)
	if strings.Contains(render(e.api.take()), "звёздами") {
		t.Fatal("кнопка автоподписки без настройки")
	}
	e.set(t, "stars.subscription", "true")
	e.press(t, userTg, 5, "bt:"+id)
	if !strings.Contains(render(e.api.take()), "ss:"+id) {
		t.Fatal("нет кнопки автоподписки")
	}
	e.press(t, userTg, 5, "ss:"+id)
	if got := render(e.api.take()); !strings.Contains(got, "XTR s:"+id+" [{Месяц 200}] period=2592000") {
		t.Fatalf("ссылка автоподписки: %s", got)
	}
	e.paid(t, userTg, &tg.SuccessfulPayment{Currency: "XTR", TotalAmount: 200, InvoicePayload: "s:" + id,
		TelegramPaymentChargeID: "rc-1", IsRecurring: true, IsFirstRecurring: true}, nil)
	u, _ := e.c.UserByTg(context.Background(), userTg)
	if s, err := e.c.Sub(context.Background(), u.ID); err != nil || s.TariffID != tr.ID {
		t.Fatalf("автоподписка не оформила тариф: %+v %v", s, err)
	}
}

func TestLinkGatewayAndCheck(t *testing.T) {
	e := payEnv(t)
	e.press(t, userTg, 5, "tpg:fake:30000")
	golden(t, "topup_link", e.api.take())
	u, _ := e.c.UserByTg(context.Background(), userTg)

	// Вернулся из кассы до вебхука: «Проверить оплату» сверяет и зачисляет.
	e.fake.Paid = map[string]bool{}
	for k := range map[string]bool{"fake-1": true, "fake-2": true, "fake-3": true} {
		e.fake.Paid[k] = true
	}
	e.text(t, userTg, "/start paid_1")
	if got := render(e.api.take()); !strings.Contains(got, "зачислен") || e.balance(t) != 30000 {
		t.Fatalf("возврат из кассы: баланс %d, %s", e.balance(t), got)
	}

	// Чужой платёж не показывается.
	other := int64(2002)
	e.text(t, other, "/start paid_1")
	if got := render(e.api.take()); strings.Contains(got, "300 ₽") {
		t.Fatalf("чужой платёж показан: %s", got)
	}
	_ = u
}

func TestDoubleClickGateway(t *testing.T) {
	e := payEnv(t)
	e.press(t, userTg, 5, "tpg:stars:30000")
	e.press(t, userTg, 5, "tpg:stars:30000")
	n := 0
	for _, c := range e.api.take() {
		if c.method == "invoice" {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("двойной клик выставил %d счетов", n)
	}
}

func TestUrgentPaymentRetried(t *testing.T) {
	e := payEnv(t)
	e.press(t, userTg, 5, "tpg:stars:30000")
	payload := strings.Fields(e.api.take()[0].text)[1]
	u := tg.Update{UpdateID: 50, Message: &tg.Message{MessageID: 1, From: &tg.User{ID: userTg}, Chat: tg.Chat{ID: userTg, Type: "private"},
		SuccessfulPayment: &tg.SuccessfulPayment{Currency: "XTR", TotalAmount: 200, InvoicePayload: payload, TelegramPaymentChargeID: "ch-u"}}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // база не отвечает: контекст уже закрыт
	if handled, err := e.b.Urgent(ctx, u); !handled || err == nil {
		t.Fatalf("сбой должен вернуть ошибку для повтора: %v %v", handled, err)
	}
	if handled, err := e.b.Urgent(context.Background(), u); !handled || err != nil {
		t.Fatalf("повтор: %v %v", handled, err)
	}
	if handled, err := e.b.Urgent(context.Background(), u); !handled || err != nil {
		t.Fatalf("второй повтор того же обновления: %v %v", handled, err)
	}
	if b := e.balance(t); b != 30000 {
		t.Fatalf("после сбоя и двух повторов баланс %d, ждали 30000", b)
	}
	if handled, _ := e.b.Urgent(context.Background(), tg.Update{Message: &tg.Message{From: &tg.User{ID: 1}, Chat: tg.Chat{Type: "private"}, Text: "hi"}}); handled {
		t.Fatal("обычное сообщение ушло в срочную обработку")
	}
}

func TestUrgentForeignPaymentReported(t *testing.T) {
	e := payEnv(t)
	e.press(t, userTg, 5, "tpg:stars:30000")
	payload := strings.Fields(e.api.take()[0].text)[1]
	e.text(t, 2002, "/start")
	u := tg.Update{Message: &tg.Message{From: &tg.User{ID: 2002}, Chat: tg.Chat{ID: 2002, Type: "private"},
		SuccessfulPayment: &tg.SuccessfulPayment{Currency: "XTR", TotalAmount: 200, InvoicePayload: payload, TelegramPaymentChargeID: "ch-x"}}}
	if handled, err := e.b.Urgent(context.Background(), u); !handled || err != nil {
		t.Fatalf("чужой платёж не должен повторяться: %v %v", handled, err)
	}
}
