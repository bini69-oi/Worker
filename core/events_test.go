package core_test

import (
	"context"
	"testing"
	"time"

	"github.com/bini69-oi/worker/core"
)

func TestTrafficLimitedOncePerDay(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.bought(t)
	ev := core.PanelEvent{Scope: "user", Name: core.EventTrafficLimited, Username: "u1001"}
	for range 3 {
		if err := e.c.HandlePanelEvent(ctx, ev); err != nil {
			t.Fatal(err)
		}
	}
	e.process(t)
	if len(e.notes.ToUsers) != 1 || e.notes.ToUsers[0].Key != "notify.traffic_limited" {
		t.Fatalf("ждали одно уведомление в день, получили %+v", e.notes.ToUsers)
	}
	e.clock.Add(24 * time.Hour)
	ev.TgID, ev.Username = 1001, ""
	if err := e.c.HandlePanelEvent(ctx, ev); err != nil {
		t.Fatal(err)
	}
	e.process(t)
	if len(e.notes.ToUsers) != 2 {
		t.Fatalf("на следующий день ждали второе уведомление, всего %d", len(e.notes.ToUsers))
	}
}

func TestPanelEventForeignUserIgnored(t *testing.T) {
	e := newEnv(t)
	for _, ev := range []core.PanelEvent{
		{Scope: "user", Name: core.EventTrafficLimited, Username: "admin"},
		{Scope: "user", Name: core.EventTrafficLimited, TgID: 777},
		{Scope: "user", Name: "user.modified", TgID: 1001},
	} {
		if err := e.c.HandlePanelEvent(context.Background(), ev); err != nil {
			t.Fatal(err)
		}
	}
	if n := len(e.st.Tasks()); n != 0 {
		t.Fatalf("чужие и ненужные события поставили задачи: %d", n)
	}
}

func TestNodeEventToAdmins(t *testing.T) {
	e := newEnv(t)
	if err := e.c.HandlePanelEvent(context.Background(), core.PanelEvent{Scope: core.ScopeNode, Name: "node.connection_lost"}); err != nil {
		t.Fatal(err)
	}
	e.process(t)
	if len(e.notes.ToAdmins) != 1 || e.notes.ToAdmins[0].Vars["event"] != "node.connection_lost" {
		t.Fatalf("событие ноды не дошло до админов: %+v", e.notes.ToAdmins)
	}
}
