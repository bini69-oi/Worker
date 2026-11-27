package core_test

import (
	"context"
	"errors"
	"strconv"
	"testing"

	"github.com/bini69-oi/worker/core"
)

func usernames(p core.Page[core.UserRow]) []string {
	var out []string
	for _, r := range p.Items {
		out = append(out, r.User.Username)
	}
	return out
}

func TestFindUsers(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t) // ivan, tg 1001
	for i, name := range []string{"petr", "a%_x", "anna"} {
		if _, err := e.c.Register(ctx, int64(2001+i), name, "ru"); err != nil {
			t.Fatal(err)
		}
	}
	ref, _ := e.c.UserByTg(ctx, 2001)
	cases := []struct {
		name         string
		text, filter string
		want         []string
	}{
		{"по ID", "2001", "", []string{"petr"}},
		{"по ID с пробелами", "  2001 ", "", []string{"petr"}},
		{"по коду рефералки", ref.RefCode, "", []string{"petr"}},
		{"по началу имени", "@an", "", []string{"anna"}},
		{"спецсимволы", "@a%_", "", []string{"a%_x"}},
		{"только процент", "@%", "", nil},
		{"нет такого", "99999", "", nil},
		{"без текста все", "", "", []string{"anna", "a%_x", "petr", "ivan"}},
		{"фильтр без подписки", "", core.FilterNone, []string{"anna", "a%_x", "petr", "ivan"}},
		{"фильтр забанен", "", core.FilterBanned, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p, err := e.c.FindUsers(ctx, owner, c.text, c.filter, 0)
			if err != nil {
				t.Fatal(err)
			}
			got := usernames(p)
			if len(got) != len(c.want) {
				t.Fatalf("нашли %v, ждали %v", got, c.want)
			}
			for i := range got {
				if got[i] != c.want[i] {
					t.Fatalf("нашли %v, ждали %v", got, c.want)
				}
			}
		})
	}
}

func TestFindUsersFilterWithSub(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	e.bought(t) // у ivan платная подписка
	if _, err := e.c.Register(ctx, 3001, "free", "ru"); err != nil {
		t.Fatal(err)
	}
	p, _ := e.c.FindUsers(ctx, owner, "", core.FilterActive, 0)
	if got := usernames(p); len(got) != 1 || got[0] != "ivan" || p.Items[0].Class(e.clock.Now()) != core.FilterActive {
		t.Errorf("с подпиской: %v", got)
	}
	p, _ = e.c.FindUsers(ctx, owner, "", core.FilterNone, 0)
	if got := usernames(p); len(got) != 1 || got[0] != "free" {
		t.Errorf("без подписки: %v", got)
	}
}

func TestFindUsersSecondPage(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	for i := range 120 {
		if _, err := e.c.Register(ctx, int64(5000+i), "u"+strconv.Itoa(i), "ru"); err != nil {
			t.Fatal(err)
		}
	}
	first, err := e.c.FindUsers(ctx, owner, "", "", 0)
	if err != nil || len(first.Items) != core.PageSize || first.Next == 0 {
		t.Fatalf("первая страница: %d, курсор %d, %v", len(first.Items), first.Next, err)
	}
	// Новый абонент после открытия первой страницы не сдвигает вторую.
	if _, err := e.c.Register(ctx, 9999, "late", "ru"); err != nil {
		t.Fatal(err)
	}
	second, err := e.c.FindUsers(ctx, owner, "", "", first.Next)
	if err != nil || len(second.Items) != core.PageSize || second.Next == 0 {
		t.Fatalf("вторая страница: %d, %v", len(second.Items), err)
	}
	if second.Items[0].User.ID >= first.Items[len(first.Items)-1].User.ID {
		t.Errorf("страницы пересеклись")
	}
	third, _ := e.c.FindUsers(ctx, owner, "", "", second.Next)
	if len(third.Items) != 121-2*core.PageSize || third.Next != 0 {
		t.Errorf("последняя страница: %d, курсор %d", len(third.Items), third.Next)
	}
}

func TestFindUsersRights(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	helperWith(t, e, 50, core.PermSupport)
	if _, err := e.c.FindUsers(ctx, 50, "", "", 0); !errors.Is(err, core.ErrForbidden) {
		t.Errorf("помощник без права users: %v", err)
	}
	helperWith(t, e, 51, core.PermUsers)
	if _, err := e.c.FindUsers(ctx, 51, "", "", 0); err != nil {
		t.Errorf("помощник с правом: %v", err)
	}
	if _, err := e.c.FindUsers(ctx, owner, "", "чужой", 0); !errors.Is(err, core.ErrInvalid) {
		t.Errorf("неизвестный фильтр: %v", err)
	}
	long := make([]byte, 100)
	for i := range long {
		long[i] = 'a'
	}
	if _, err := e.c.FindUsers(ctx, owner, string(long), "", 0); !errors.Is(err, core.ErrTooLong) {
		t.Errorf("длинный запрос: %v", err)
	}
}
