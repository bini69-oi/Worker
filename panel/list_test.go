package panel_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/bini69-oi/worker/panel"
)

const listToken = "list-token-value"

// listPanel это панель, которая отдаёт записанные ответы из testdata/list: pages сопоставляет
// смещение start и файл. Без токена отвечает 401. Второе значение это запросы к /api/users.
func listPanel(t *testing.T, version string, pages map[int]string) (*httptest.Server, *[]string) {
	t.Helper()
	var queries []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+listToken {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/system/metadata":
			_, _ = w.Write([]byte(`{"response":{"version":"` + version + `"}}`))
		case "/api/users":
			queries = append(queries, r.URL.RawQuery)
			start, _ := strconv.Atoi(r.URL.Query().Get("start"))
			file, ok := pages[start]
			if !ok {
				_, _ = w.Write([]byte(`{"response":{"total":0,"users":[]}}`))
				return
			}
			data, err := os.ReadFile(filepath.Join("testdata", "list", file))
			if err != nil {
				t.Error(err)
			}
			_, _ = w.Write(data)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &queries
}

func TestListUsersPages(t *testing.T) {
	cases := []struct {
		name, version string
		pages         map[int]string
		wantID        string
		wantUUID      string
		wantSquad     string
		wantReset     panel.Strategy
		junk          string
	}{
		{"v2", "2.8.4", map[int]string{0: "v2_page1.json", 2: "v2_page2.json"},
			"3f6b1c2e-1a2b-4c3d-8e4f-0123456789ab", "3f6b1c2e-1a2b-4c3d-8e4f-0123456789ab",
			"22222222-2222-4222-8222-222222222222", panel.Monthly, "плохой uuid"},
		{"v3", "3.4.5", map[int]string{0: "v3_page1.json", 2: "v3_page2.json"},
			"101", "", "11111111-1111-4111-8111-111111111111", panel.MonthRolling, "плохой лимит трафика"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, queries := listPanel(t, tc.version, tc.pages)
			c := panel.New(srv.URL, listToken, nil)
			var users []panel.ListedUser
			var skipped []panel.SkippedUser
			err := c.EachUsersPage(context.Background(), 2, func(p panel.UsersPage) error {
				users = append(users, p.Users...)
				skipped = append(skipped, p.Skipped...)
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			if len(*queries) < 2 || (*queries)[0] != "size=2&start=0" || (*queries)[1] != "size=2&start=2" {
				t.Fatalf("queries %q", *queries)
			}
			first := users[0]
			if first.PanelID != tc.wantID || first.UUID != tc.wantUUID || first.Username != "alice" ||
				first.TelegramID != 4242 || first.Status != "ACTIVE" || first.TrafficLimitBytes != 50<<30 ||
				first.DeviceLimit != 3 || first.TrafficReset != tc.wantReset ||
				first.SubURL != "https://sub.example.com/abc123" ||
				!first.ExpireAt.Equal(time.Date(2027, 3, 15, 0, 0, 0, 0, time.UTC)) ||
				!first.CreatedAt.Equal(time.Date(2026, 1, 10, 10, 0, 0, 0, time.UTC)) {
				t.Fatalf("first user: %+v", first)
			}
			if got := first.Squads[len(first.Squads)-1]; got != tc.wantSquad {
				t.Fatalf("squads %v", first.Squads)
			}
			bob := users[1]
			if bob.Username != "bob_no_tg" || bob.TelegramID != 0 || bob.DeviceLimit != 0 || bob.TrafficLimitBytes != 0 || len(bob.Squads) != 0 {
				t.Fatalf("user without telegram: %+v", bob)
			}
			if len(skipped) != 1 || skipped[0].Reason != tc.junk {
				t.Fatalf("skipped %+v", skipped)
			}
		})
	}
}

func TestListUsersV2NumbersAsStrings(t *testing.T) {
	srv, _ := listPanel(t, "2.8.4", map[int]string{2: "v2_page2.json"})
	page, err := panel.New(srv.URL, listToken, nil).ListUsers(context.Background(), 2, 2)
	if err != nil {
		t.Fatal(err)
	}
	carol := page.Users[0]
	if carol.Username != "carol" || carol.TelegramID != 777 || carol.TrafficLimitBytes != 1<<30 ||
		carol.Squads[0] != "33333333-3333-4333-8333-333333333333" || page.Total != 4 || page.Fetched != 2 {
		t.Fatalf("%+v total %d fetched %d", carol, page.Total, page.Fetched)
	}
}

func TestListUsersJunkIsSkippedWithReason(t *testing.T) {
	srv, _ := listPanel(t, "2.8.4", map[int]string{0: "v2_junk.json"})
	page, err := panel.New(srv.URL, listToken, nil).ListUsers(context.Background(), 0, 500)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Users) != 1 || page.Users[0].Username != "ok_user" || page.Fetched != 12 {
		t.Fatalf("users %+v fetched %d", page.Users, page.Fetched)
	}
	want := map[string]string{
		"bad_expire":   "плохая дата окончания",
		"neg_limit":    "плохой лимит трафика",
		"neg_devices":  "плохой лимит устройств",
		"bad_strategy": "плохая стратегия сброса",
		"bad_squad":    "плохой uuid сквада",
		"bad_url":      "плохая ссылка подписки",
		"bad_tg":       "плохой Telegram ID",
		"bad_created":  "плохая дата создания",
		"bad_status":   "плохой статус",
		"":             "плохое имя",
	}
	if len(page.Skipped) != 11 {
		t.Fatalf("skipped %d: %+v", len(page.Skipped), page.Skipped)
	}
	for _, s := range page.Skipped {
		if w, ok := want[s.Username]; ok {
			if s.Reason != w {
				t.Errorf("%q: reason %q, want %q", s.Username, s.Reason, w)
			}
			delete(want, s.Username)
		} else if s.Reason != "запись панели не объект" {
			t.Errorf("unexpected skip %+v", s)
		}
	}
	if len(want) != 0 {
		t.Fatalf("not skipped: %v", want)
	}
}

func TestListUsersLongStringIsSkipped(t *testing.T) {
	body := `{"response":{"total":1,"users":[{"uuid":"6c9e4f51-4d5e-4f60-b172-3456789abcde","username":"` +
		strings.Repeat("я", 257) + `","status":"ACTIVE","trafficLimitStrategy":"NO_RESET","expireAt":"2027-03-15T00:00:00Z","subscriptionUrl":"https://s.example/x","createdAt":"2026-01-10T10:00:00Z"}]}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/users" {
			_, _ = w.Write([]byte(body))
			return
		}
		_, _ = w.Write([]byte(`{"response":{"version":"2.8.4"}}`))
	}))
	defer srv.Close()
	page, err := panel.New(srv.URL, listToken, nil).ListUsers(context.Background(), 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Users) != 0 || len(page.Skipped) != 1 || page.Skipped[0].Username != "" || page.Skipped[0].Reason != "плохое имя" {
		t.Fatalf("%+v", page)
	}
}

func TestListUsersBadArguments(t *testing.T) {
	c := panel.New("http://127.0.0.1:1", listToken, nil)
	for _, tc := range [][2]int{{-1, 10}, {0, 0}, {0, 501}} {
		if _, err := c.ListUsers(context.Background(), tc[0], tc[1]); err == nil {
			t.Errorf("start %d size %d: expected an error", tc[0], tc[1])
		}
	}
}

func TestListUsersBadAnswer(t *testing.T) {
	for _, body := range []string{`{"response":{"total":1}}`, `{"response":{"users":[],"total":"x"}}`} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/api/users" {
				_, _ = w.Write([]byte(body))
				return
			}
			_, _ = w.Write([]byte(`{"response":{"version":"3.4.5"}}`))
		}))
		_, err := panel.New(srv.URL, listToken, nil).ListUsers(context.Background(), 0, 10)
		srv.Close()
		if err == nil {
			t.Errorf("%s: expected an error", body)
		}
	}
}

func TestListUsersBadTokenIsNotLeaked(t *testing.T) {
	srv, _ := listPanel(t, "3.4.5", nil)
	const secret = "wrong-secret-token"
	_, err := panel.New(srv.URL, secret, nil).ListUsers(context.Background(), 0, 10)
	if err == nil || strings.Contains(err.Error(), secret) {
		t.Fatalf("err %v", err)
	}
	if !panel.IsAuth(err) || panel.IsRetryable(err) {
		t.Fatalf("401 is an auth error and not retryable: %v", err)
	}
}

func TestListUsersStopsOnEmptyPage(t *testing.T) {
	// Панель врёт про total: после первой страницы пусто, цикл не должен крутиться вечно.
	srv, queries := listPanel(t, "2.8.4", map[int]string{0: "v2_page1.json"})
	pages := 0
	err := panel.New(srv.URL, listToken, nil).EachUsersPage(context.Background(), 2, func(panel.UsersPage) error {
		pages++
		return nil
	})
	if err != nil || pages != 1 || len(*queries) != 2 {
		t.Fatalf("pages %d queries %v err %v", pages, *queries, err)
	}
}
