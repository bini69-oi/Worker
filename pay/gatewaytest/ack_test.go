package gatewaytest

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/bini69-oi/worker/core"
)

// ackFake это тестовая касса, которая ждёт в ответе своё слово.
type ackFake struct{ *Fake }

func (ackFake) Ack(core.GatewayEvent) string { return "YES" }

func TestAckWordInResponse(t *testing.T) {
	g := ackFake{&Fake{Secret: "s"}}
	e := newEnv(t, g)
	p, _, err := e.c.CreatePayment(context.Background(), e.user.ID, g.Name(), 30000)
	if err != nil {
		t.Fatal(err)
	}
	for range 2 { // и первое уведомление, и повтор получают ответ кассы
		w := httptest.NewRecorder()
		e.h.ServeHTTP(w, g.Hook(p, p.ExtAmount, false))
		if w.Code != 200 || w.Body.String() != "YES" {
			t.Fatalf("ответ кассе: %d %q", w.Code, w.Body.String())
		}
	}
}
