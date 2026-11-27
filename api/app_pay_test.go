package api

import (
	"strconv"
	"testing"
	"time"
)

func TestTopupLink(t *testing.T) {
	e := newAppEnv(t)
	e.enable("fake")
	tok := e.login(31)
	rec := e.as(tok, "POST", "/api/app/topup", `{"gateway":"fake","amount":30000}`)
	if rec.Code != 200 {
		t.Fatalf("пополнение: %d %s", rec.Code, rec.Body)
	}
	golden(t, "topup_link", rec.Body.Bytes())
}

func TestTopupManual(t *testing.T) {
	e := newAppEnv(t)
	tok := e.login(31)
	rec := e.as(tok, "POST", "/api/app/topup", `{"gateway":"manual","amount":30000}`)
	if rec.Code != 200 {
		t.Fatalf("ручная касса: %d %s", rec.Code, rec.Body)
	}
	golden(t, "topup_manual", rec.Body.Bytes())
}

func TestTopupStars(t *testing.T) {
	e := newAppEnv(t)
	e.enable("stars")
	tok := e.login(31)
	rec := e.as(tok, "POST", "/api/app/topup", `{"gateway":"stars","amount":30000}`)
	if rec.Code != 200 {
		t.Fatalf("Stars: %d %s", rec.Code, rec.Body)
	}
	var out TopupResponse
	decode(t, rec, &out)
	if len(e.invoices) != 1 || e.invoices[0].Payload != "p:"+strconv.FormatInt(out.PaymentID, 10) || out.InvoiceLink == "" || e.invoices[0].Stars != out.ExtAmount {
		t.Fatalf("счёт Stars: %+v ответ %+v", e.invoices, out)
	}
	if e.invoices[0].Title == "" || e.invoices[0].Title == "app.stars_title" {
		t.Fatalf("название счёта: %q", e.invoices[0].Title)
	}
}

func TestTopupErrors(t *testing.T) {
	e := newAppEnv(t)
	e.enable("fake")
	tok := e.login(31)
	if err := e.cfg.Set("fake.min", "10000"); err != nil {
		t.Fatal(err)
	}
	cases := map[string]struct {
		body string
		code int
	}{
		"касса выключена":    {`{"gateway":"stars","amount":30000}`, 409},
		"нет такой кассы":    {`{"gateway":"nope","amount":30000}`, 409},
		"сумма ниже предела": {`{"gateway":"fake","amount":5}`, 400},
		"нулевая сумма":      {`{"gateway":"fake","amount":0}`, 400},
		"минус":              {`{"gateway":"fake","amount":-5}`, 400},
		"огромная сумма":     {`{"gateway":"fake","amount":100000000000}`, 400},
		"лишнее поле":        {`{"gateway":"fake","amount":30000,"user_id":2}`, 400},
	}
	for name, c := range cases {
		if rec := e.as(tok, "POST", "/api/app/topup", c.body); rec.Code != c.code {
			t.Errorf("%s: %d %s, ждали %d", name, rec.Code, rec.Body, c.code)
		}
	}
	var body ErrorBody
	decode(t, e.as(tok, "POST", "/api/app/topup", `{"gateway":"fake","amount":5}`), &body)
	if body.Limit == nil || body.Limit.Min != 10000 {
		t.Fatalf("в ответе нет пределов кассы: %+v", body)
	}
}

func TestPaymentOtherUser(t *testing.T) {
	e := newAppEnv(t)
	e.enable("fake")
	a, b := e.login(31), e.login(32)
	var out TopupResponse
	decode(t, e.as(a, "POST", "/api/app/topup", `{"gateway":"fake","amount":30000}`), &out)
	id := strconv.FormatInt(out.PaymentID, 10)
	rec := e.as(b, "GET", "/api/app/payments/"+id, "")
	if rec.Code != 404 {
		t.Fatalf("чужой платёж: %d %s", rec.Code, rec.Body)
	}
	if rec2 := e.as(b, "GET", "/api/app/payments/999", ""); rec2.Body.String() != rec.Body.String() {
		t.Fatalf("чужой платёж отличим от несуществующего: %s и %s", rec.Body, rec2.Body)
	}
	if rec := e.as(a, "GET", "/api/app/payments/abc", ""); rec.Code != 404 {
		t.Fatalf("мусорный номер: %d", rec.Code)
	}
	if rec := e.as(a, "GET", "/api/app/payments/"+id, ""); rec.Code != 200 {
		t.Fatalf("свой платёж: %d", rec.Code)
	}
}

func TestPaymentWebhookLate(t *testing.T) {
	e := newAppEnv(t)
	e.enable("fake")
	tok := e.login(31)
	var out TopupResponse
	decode(t, e.as(tok, "POST", "/api/app/topup", `{"gateway":"fake","amount":30000}`), &out)
	id := strconv.FormatInt(out.PaymentID, 10)

	var p PaymentResponse
	decode(t, e.as(tok, "GET", "/api/app/payments/"+id, ""), &p)
	if p.Status != "pending" {
		t.Fatalf("ещё не оплачен: %+v", p)
	}
	// Абонент оплатил, вебхук не пришёл: касса при сверке отвечает «оплачен». Сверка не чаще раза в 3 секунды.
	e.fake.Paid["fake-"+id] = true
	decode(t, e.as(tok, "GET", "/api/app/payments/"+id, ""), &p)
	if p.Status != "pending" {
		t.Fatalf("сверка чаще раза в 3 секунды: %+v", p)
	}
	e.clock.Add(4 * time.Second)
	rec := e.as(tok, "GET", "/api/app/payments/"+id, "")
	decode(t, rec, &p)
	if p.Status != "paid" {
		t.Fatalf("после сверки: %s", rec.Body)
	}
	golden(t, "payment_paid", rec.Body.Bytes())
	u, _ := e.c.UserByTg(t.Context(), 31)
	if u.Balance != 30000 {
		t.Fatalf("баланс после сверки: %d", u.Balance)
	}
}
