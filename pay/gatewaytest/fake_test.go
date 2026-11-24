package gatewaytest

import (
	"net/http"
	"testing"

	"github.com/bini69-oi/worker/core"
)

func TestFakePassesSuite(t *testing.T) {
	f := &Fake{Secret: "s3cret"}
	Run(t, Case{
		Gateway:      f,
		Unconfigured: &Fake{},
		Amount:       30000,
		Paid:         func(p core.Payment, amount int64) *http.Request { return f.Hook(p, amount, false) },
		Forged:       func(p core.Payment, amount int64) *http.Request { return f.Hook(p, amount, true) },
	})
}
