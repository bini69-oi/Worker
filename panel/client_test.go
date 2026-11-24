package panel_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/bini69-oi/worker/panel"
	"github.com/bini69-oi/worker/panel/paneltest"
)

var generations = []struct {
	name string
	gen  paneltest.Generation
}{
	{"v2", paneltest.V2},
	{"v3", paneltest.V3},
}

func newClient(p *paneltest.Panel) *panel.Client {
	return panel.New(p.URL(), paneltest.Token, nil)
}

func sampleState() panel.State {
	return panel.State{
		Username:          "user_1",
		TelegramID:        4242,
		ExpireAt:          time.Now().Add(30 * 24 * time.Hour).UTC().Truncate(time.Second),
		TrafficLimitBytes: 100 << 30,
		Strategy:          panel.Monthly,
		DeviceLimit:       3,
		Squads:            []string{"11111111-1111-1111-1111-111111111111"},
		Enabled:           true,
	}
}

func TestSyncCreatesUser(t *testing.T) {
	for _, g := range generations {
		t.Run(g.name, func(t *testing.T) {
			p := paneltest.New(t, g.gen)
			state := sampleState()
			res, err := newClient(p).Sync(context.Background(), state)
			if err != nil {
				t.Fatal(err)
			}
			if !res.Created || res.PanelID == "" || res.SubscriptionURL == "" {
				t.Fatalf("bad result: %+v", res)
			}
			if !res.ExpireAt.Equal(state.ExpireAt) {
				t.Fatalf("expire %v, want %v", res.ExpireAt, state.ExpireAt)
			}
			users := p.Users()
			if len(users) != 1 || users[0].DeviceLimit != 3 || users[0].TelegramID != 4242 || users[0].Strategy != "MONTH" {
				t.Fatalf("panel has %+v", users)
			}
		})
	}
}

func TestSyncTwiceKeepsOneUser(t *testing.T) {
	for _, g := range generations {
		t.Run(g.name, func(t *testing.T) {
			p := paneltest.New(t, g.gen)
			c := newClient(p)
			state := sampleState()
			first, err := c.Sync(context.Background(), state)
			if err != nil {
				t.Fatal(err)
			}
			// Второй раз ни сохранённого идентификатора, ни ответа первого вызова.
			second, err := c.Sync(context.Background(), state)
			if err != nil {
				t.Fatal(err)
			}
			if second.Created || len(p.Users()) != 1 {
				t.Fatalf("second sync created a user: %+v", second)
			}
			if second.PanelID != first.PanelID || !second.ExpireAt.Equal(first.ExpireAt) {
				t.Fatalf("first %+v, second %+v", first, second)
			}
			if second.SubscriptionURL != first.SubscriptionURL {
				t.Fatal("subscription link changed")
			}
			state.PanelID = first.PanelID
			if third, err := c.Sync(context.Background(), state); err != nil || third.Created {
				t.Fatalf("sync by saved id: %+v %v", third, err)
			}
		})
	}
}

func TestSyncRestoresManualEdit(t *testing.T) {
	for _, g := range generations {
		t.Run(g.name, func(t *testing.T) {
			p := paneltest.New(t, g.gen)
			c := newClient(p)
			state := sampleState()
			if _, err := c.Sync(context.Background(), state); err != nil {
				t.Fatal(err)
			}
			p.Edit(state.Username, func(u *paneltest.User) { u.ExpireAt = u.ExpireAt.AddDate(1, 0, 0) })
			res, err := c.Sync(context.Background(), state)
			if err != nil {
				t.Fatal(err)
			}
			if !res.ExpireAt.Equal(state.ExpireAt) || !p.Users()[0].ExpireAt.Equal(state.ExpireAt) {
				t.Fatalf("expire %v, want %v", res.ExpireAt, state.ExpireAt)
			}
		})
	}
}

func TestSyncDisablesUser(t *testing.T) {
	p := paneltest.New(t, paneltest.V3)
	state := sampleState()
	state.Enabled = false
	state.ExpireAt = time.Now().Add(-time.Hour) // 3.x не принимает прошлое, клиент сдвигает срок
	if _, err := newClient(p).Sync(context.Background(), state); err != nil {
		t.Fatal(err)
	}
	if got := p.Users()[0].Status; got != "DISABLED" {
		t.Fatalf("status %s", got)
	}
}

func TestSyncFindsUserAfterPanelUpgrade(t *testing.T) {
	p := paneltest.New(t, paneltest.V3)
	c := newClient(p)
	state := sampleState()
	if _, err := c.Sync(context.Background(), state); err != nil {
		t.Fatal(err)
	}
	state.PanelID = "11111111-2222-3333-4444-555555555555" // старый uuid из 2.x
	res, err := c.Sync(context.Background(), state)
	if err != nil || res.Created || len(p.Users()) != 1 {
		t.Fatalf("res %+v err %v users %d", res, err, len(p.Users()))
	}
}

func TestResetDevicesAndRevokeLink(t *testing.T) {
	for _, g := range generations {
		t.Run(g.name, func(t *testing.T) {
			p := paneltest.New(t, g.gen)
			c := newClient(p)
			ctx := context.Background()
			res, err := c.Sync(ctx, sampleState())
			if err != nil {
				t.Fatal(err)
			}
			p.AddDevice("user_1", "hwid-a")
			p.AddDevice("user_1", "hwid-b")
			if list, err := c.Devices(ctx, res.PanelID); err != nil || len(list) != 2 {
				t.Fatalf("devices %v %v", list, err)
			}
			if err := c.ResetDevices(ctx, res.PanelID); err != nil {
				t.Fatal(err)
			}
			if left := p.Users()[0].Devices; len(left) != 0 {
				t.Fatalf("devices left: %v", left)
			}
			if n := p.Calls("POST /api/hwid/devices/delete-all"); n != 1 {
				t.Fatalf("сброс устройств должен идти одним запросом delete-all, было %d", n)
			}
			fresh, err := c.RevokeLink(ctx, res.PanelID)
			if err != nil {
				t.Fatal(err)
			}
			if fresh.SubscriptionURL == "" || fresh.SubscriptionURL == res.SubscriptionURL {
				t.Fatalf("link not changed: %s", fresh.SubscriptionURL)
			}
		})
	}
}

func TestVersion(t *testing.T) {
	for _, g := range generations {
		t.Run(g.name, func(t *testing.T) {
			v, err := newClient(paneltest.New(t, g.gen)).Version(context.Background())
			if err != nil || v.Generation() != int(g.gen) {
				t.Fatalf("version %+v err %v", v, err)
			}
		})
	}
}

func TestErrorKinds(t *testing.T) {
	p := paneltest.New(t, paneltest.V2)
	c := newClient(p)
	p.SetDown(true)
	_, err := c.Sync(context.Background(), sampleState())
	if err == nil || !panel.IsRetryable(err) {
		t.Fatalf("503 must be retryable: %v", err)
	}
	p.SetDown(false)
	p.Force(400)
	_, err = c.Sync(context.Background(), sampleState())
	if err == nil || panel.IsRetryable(err) {
		t.Fatalf("400 must not be retryable: %v", err)
	}
}

func TestPanelUnreachableIsRetryable(t *testing.T) {
	p := paneltest.New(t, paneltest.V2)
	url := p.URL()
	c := panel.New(url, paneltest.Token, nil)
	p.Close()
	if _, err := c.Sync(context.Background(), sampleState()); !panel.IsRetryable(err) {
		t.Fatalf("closed panel must be retryable: %v", err)
	}
}

func TestBadTokenIsNotLeaked(t *testing.T) {
	p := paneltest.New(t, paneltest.V3)
	const secret = "super-secret-token-value"
	c := panel.New(p.URL(), secret, nil)
	_, err := c.Sync(context.Background(), sampleState())
	if err == nil {
		t.Fatal("expected an error")
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("token leaked: %v", err)
	}
	if panel.IsRetryable(err) || !panel.IsAuth(err) {
		t.Fatalf("401 is an auth error and not retryable: %v", err)
	}
}

func TestSyncRejectsEmptyState(t *testing.T) {
	c := panel.New("http://127.0.0.1:1", "x", nil)
	if _, err := c.Sync(context.Background(), panel.State{}); err == nil {
		t.Fatal("expected an error")
	}
}
