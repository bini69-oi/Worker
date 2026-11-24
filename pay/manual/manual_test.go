package manual

import (
	"testing"

	"github.com/bini69-oi/worker/core/coretest"
	"github.com/bini69-oi/worker/pay/gatewaytest"
)

func TestManualSuite(t *testing.T) {
	cfg := coretest.NewConfig(t)
	if err := cfg.Set("manual.details", "Карта 0000, в комментарии ваш номер"); err != nil {
		t.Fatal(err)
	}
	gatewaytest.Run(t, gatewaytest.Case{
		Gateway:      New(cfg),
		Unconfigured: New(coretest.NewConfig(t)),
		Amount:       30000,
	})
}
