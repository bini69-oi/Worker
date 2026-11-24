package core_test

import (
	"context"
	"errors"
	"testing"

	"github.com/bini69-oi/worker/core"
)

func TestAdminAdjust(t *testing.T) {
	c, st, u := newCore(t)
	ctx := context.Background()

	if _, err := c.AdminAdjust(ctx, owner, u.ID, 10000, "  "); !errors.Is(err, core.ErrNeedComment) {
		t.Fatalf("без комментария ждали ErrNeedComment, получили %v", err)
	}
	e, err := c.AdminAdjust(ctx, owner, u.ID, 10000, "компенсация")
	if err != nil {
		t.Fatal(err)
	}
	if e.AdminID != owner || e.Comment != "компенсация" || e.Reason != core.ReasonAdmin {
		t.Fatalf("в истории нет админа или причины: %+v", e)
	}
	if b := balance(t, st, u.ID); b != 10000 {
		t.Fatalf("баланс %d, ждали 10000", b)
	}
	if _, err := c.AdminAdjust(ctx, owner, u.ID, -20000, "ошибка"); !errors.Is(err, core.ErrNotEnoughMoney) {
		t.Fatalf("списание больше баланса: ждали ErrNotEnoughMoney, получили %v", err)
	}
}
