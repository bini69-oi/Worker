package panel_test

import (
	"context"
	"testing"

	"github.com/bini69-oi/worker/core"
	"github.com/bini69-oi/worker/panel"
	"github.com/bini69-oi/worker/panel/paneltest"
)

func TestAdapterNotFound(t *testing.T) {
	p := paneltest.New(t, paneltest.V2)
	a := panel.CoreAdapter{Client: newClient(p)}
	if _, err := a.Get(context.Background(), "00000000-0000-0000-0000-000000000000"); err != core.ErrNotFound {
		t.Fatalf("ждали core.ErrNotFound, получили %v", err)
	}
}
