package bot

import (
	"context"
	"strconv"
	"strings"
	"testing"

	"github.com/bini69-oi/worker/core"
	"github.com/bini69-oi/worker/tg"
)

// TestWholeFlow проходит путь абонента целиком: /start, пробный период, подключение, пополнение звёздами,
// покупка, уведомление, вопрос в поддержку и ответ админа реплаем.
func TestWholeFlow(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.set(t, "trial.enabled", "true", "trial.days", "3", "stars.enabled", "true")
	tr := e.tariff(t, "Месяц", core.Period{Days: 30, Price: 30000})
	e.bind(t, chat)

	e.text(t, userTg, "/start")
	if got := render(e.api.take()); !strings.Contains(got, "Попробовать бесплатно") {
		t.Fatalf("меню без пробного: %s", got)
	}

	e.press(t, userTg, 101, "trial")
	e.sync(t) // панель создаёт абонента, админам уходит «взял пробный»
	all := render(e.api.take())
	if !strings.Contains(all, "Пробный период на 3 дн.") || !strings.Contains(all, "взял пробный период") {
		t.Fatalf("пробный период: %s", all)
	}
	e.press(t, userTg, 101, "con")
	if got := render(e.api.take()); !strings.Contains(got, "Добавить в Happ") {
		t.Fatalf("подключение: %s", got)
	}

	// Покупка: денег нет, пополняем звёздами ровно на недостачу, подписка оформляется сама.
	id := strconv.FormatInt(tr.ID, 10)
	e.press(t, userTg, 101, "bp:"+id+":30")
	if got := render(e.api.take()); !strings.Contains(got, "не хватает <b>300 ₽</b>") {
		t.Fatalf("недостача: %s", got)
	}
	e.press(t, userTg, 101, "tpg:stars:30000")
	inv := e.api.take()
	payload := strings.Fields(inv[0].text)[1]
	if got := e.precheckout(t, userTg, "XTR", payload, 200); got != "ok=true " {
		t.Fatalf("проверка перед оплатой: %s", got)
	}
	e.paid(t, userTg, &tg.SuccessfulPayment{Currency: "XTR", TotalAmount: 200, InvoicePayload: payload, TelegramPaymentChargeID: "ch-flow"}, nil)
	e.sync(t)
	got := render(e.api.take())
	if !strings.Contains(got, "подписка на 30 дн. оформлена") || !strings.Contains(got, "пополнил на <b>300 ₽</b>") {
		t.Fatalf("после оплаты абонент и админы не узнали: %s", got)
	}
	u, _ := e.c.UserByTg(ctx, userTg)
	if s, _ := e.c.Sub(ctx, u.ID); s.TariffID != tr.ID || s.Trial {
		t.Fatalf("подписка после оплаты: %+v", s)
	}

	// Поддержка: вопрос в тему, ответ админа реплаем уходит абоненту.
	e.text(t, userTg, "Не работает на айфоне")
	e.api.take()
	ch, _ := e.c.AdminChat(ctx)
	e.groupIn(t, chat, owner, int(ch.Topics[core.TopicSupport]), e.lastSupportLink(t), "Обновите приложение")
	if got := render(e.api.take()); !strings.Contains(got, "== copy chat=1001") {
		t.Fatalf("ответ поддержки не дошёл: %s", got)
	}
}
