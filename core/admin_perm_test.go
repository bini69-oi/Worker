package core_test

import (
	"context"
	"errors"
	"testing"

	"github.com/bini69-oi/worker/core"
)

// helperWith заводит помощника с правами через «Да» владельца.
func helperWith(t *testing.T, e *env, tg int64, perms ...core.Perm) {
	t.Helper()
	ctx := context.Background()
	id, err := e.c.RequestAddHelper(ctx, owner, tg, perms, "helper password 1")
	if err != nil {
		t.Fatal(err)
	}
	if err := e.c.AnswerConfirm(ctx, id, owner, true); err != nil {
		t.Fatal(err)
	}
}

func TestRequire(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	helperWith(t, e, 50, core.PermUsers)
	cases := []struct {
		name string
		tg   int64
		perm core.Perm
		want error
	}{
		{"владелец, любое право", owner, core.PermTariffs, nil},
		{"владелец, только владельцу", owner, core.PermOwner, nil},
		{"помощник с правом", 50, core.PermUsers, nil},
		{"помощник без права", 50, core.PermTariffs, core.ErrForbidden},
		{"помощник, только владельцу", 50, core.PermOwner, core.ErrOwnerOnly},
		{"любой админ", 50, "", nil},
		{"не админ", 777, "", core.ErrForbidden},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			before := len(e.st.Audit())
			a, err := e.c.Require(ctx, c.tg, c.perm, "test")
			if !errors.Is(err, c.want) || (c.want == nil && a.TgID != c.tg) {
				t.Fatalf("ошибка %v, ждали %v", err, c.want)
			}
			denied := len(e.st.Audit()) - before
			if (c.want != nil) != (denied == 1) {
				t.Errorf("записей об отказе %d при ошибке %v", denied, c.want)
			}
		})
	}
	// Отказ приходит владельцу тревогой.
	found := false
	for _, n := range adminTasks(t, e) {
		found = found || n.Key == "admin.denied"
	}
	if !found {
		t.Errorf("тревоги об отказе нет")
	}
}
