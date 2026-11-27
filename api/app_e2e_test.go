package api

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/bini69-oi/worker/core"
)

// TestEndToEnd проходит путь абонента через HTTP: вход в браузере через бота, пробный период, пополнение,
// покупка, устройства, выход. Бот заменён вызовами ядра, которые он делает по нажатиям.
func TestEndToEnd(t *testing.T) {
	e := newAppEnv(t)
	e.enable("fake")
	if err := e.cfg.Set("trial.enabled", "true"); err != nil {
		t.Fatal(err)
	}
	tr := e.tariff("Месяц", 25000, 3, 100)
	ctx := t.Context()

	// Абонент уже есть в боте (нажал /start).
	u, err := e.c.Register(ctx, 81, "ivan", "ru")
	if err != nil {
		t.Fatal(err)
	}

	// 1. Сайт начинает вход, абонент открывает ссылку в боте и нажимает код с сайта.
	rec := e.req("POST", "/api/app/login", "{}")
	var st LoginStartResponse
	decode(t, rec, &st)
	nonce := strings.TrimPrefix(st.Link, "https://t.me/worker_bot?start=web_")
	prompt, err := e.c.WebLoginOpen(ctx, nonce, u.TgID)
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := e.c.WebLoginAnswer(ctx, prompt.ID, u.TgID, st.Code); err != nil || !ok {
		t.Fatalf("нажатие кода: %v %v", ok, err)
	}
	rec = poll(e, st.ID)
	c := cookieNamed(rec, SessionCookie)
	if c == nil {
		t.Fatalf("после подтверждения нет cookie: %s", rec.Body)
	}
	call := func(method, target, body string) *httptest.ResponseRecorder {
		t.Helper()
		e.clock.Add(300 * time.Millisecond)
		return e.req(method, target, body, "Cookie", SessionCookie+"="+c.Value, "X-Worker", "1", "Content-Type", "application/json")
	}

	// 2. Главная: подписки нет, пробный доступен.
	var me MeResponse
	decode(t, call("GET", "/api/app/me", ""), &me)
	if me.Sub != nil || !me.TrialAvailable || me.Balance != 0 {
		t.Fatalf("главная до всего: %+v", me)
	}

	// 3. Пробный период.
	if rec := call("POST", "/api/app/trial", "{}"); rec.Code != 200 {
		t.Fatalf("пробный: %d %s", rec.Code, rec.Body)
	}
	e.process() // панель создаёт абонента

	// 4. Покупка без денег: недостача и запомненный выбор.
	buy := `{"tariff_id":` + itoa(tr.ID) + `,"days":30}`
	rec = call("POST", "/api/app/buy", buy)
	var bad ErrorBody
	decode(t, rec, &bad)
	if rec.Code != 402 || bad.Money == nil || bad.Money.Missing != 25000 {
		t.Fatalf("покупка без денег: %d %+v", rec.Code, bad)
	}

	// 5. Пополнение на недостачу, оплата приходит, подписка выдаётся сама из запомненного выбора.
	var top TopupResponse
	decode(t, call("POST", "/api/app/topup", `{"gateway":"fake","amount":25000}`), &top)
	e.fake.Paid["fake-"+itoa(top.PaymentID)] = true
	e.clock.Add(4 * time.Second)
	var pay PaymentResponse
	decode(t, call("GET", "/api/app/payments/"+itoa(top.PaymentID), ""), &pay)
	if pay.Status != "paid" {
		t.Fatalf("платёж: %+v", pay)
	}
	e.process()
	decode(t, call("GET", "/api/app/me", ""), &me)
	if me.Sub == nil || me.Sub.State != "active" || me.Sub.Trial || me.Sub.Tariff != "Месяц" || me.Balance != 0 {
		t.Fatalf("главная после оплаты: %+v", me)
	}

	// 6. Устройства: список, удаление одного.
	e.panel.AddFullDevice(e.ref(81), core.PanelDevice{HWID: "hw-1", Platform: "iOS", Model: "iPhone", CreatedAt: t0})
	e.panel.AddFullDevice(e.ref(81), core.PanelDevice{HWID: "hw-2", Platform: "Android", Model: "Pixel", CreatedAt: t0})
	var devs DevicesResponse
	decode(t, call("GET", "/api/app/devices", ""), &devs)
	if len(devs.Devices) != 2 {
		t.Fatalf("устройства: %+v", devs)
	}
	if rec := call("DELETE", "/api/app/devices/hw-1", ""); rec.Code != 200 {
		t.Fatalf("удаление устройства: %d", rec.Code)
	}
	decode(t, call("GET", "/api/app/devices", ""), &devs)
	if len(devs.Devices) != 1 || devs.Devices[0].HWID != "hw-2" {
		t.Fatalf("после удаления: %+v", devs)
	}

	// 7. История и подключение.
	var h HistoryResponse
	decode(t, call("GET", "/api/app/history", ""), &h)
	if len(h.Items) != 2 || h.Items[0].Reason != "buy" || h.Items[1].Reason != "topup" {
		t.Fatalf("история: %+v", h)
	}
	if rec := call("GET", "/api/app/connect", ""); rec.Code != 200 || !strings.Contains(rec.Body.String(), "/dl/happ/") {
		t.Fatalf("подключение: %d %s", rec.Code, rec.Body)
	}

	// 8. Выход: cookie стёрта, старая больше не работает.
	out := call("POST", "/api/app/logout", "{}")
	if out.Code != 200 {
		t.Fatalf("выход: %d", out.Code)
	}
	if rec := call("GET", "/api/app/me", ""); rec.Code != 401 {
		t.Fatalf("после выхода: %d", rec.Code)
	}
}
