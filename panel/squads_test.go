package panel_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bini69-oi/worker/panel"
)

func TestSquads(t *testing.T) {
	var auth, path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth, path = r.Header.Get("Authorization"), r.URL.Path
		_, _ = w.Write([]byte(`{"response":{"total":2,"internalSquads":[
			{"uuid":"u-1","name":"Европа","info":{"membersCount":12,"inboundsCount":2}},
			{"uuid":"u-2","name":"Азия","info":{"membersCount":0,"inboundsCount":1}}]}}`))
	}))
	defer srv.Close()
	got, err := panel.New(srv.URL, "tok", nil).Squads(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if path != "/api/internal-squads" || auth != "Bearer tok" {
		t.Errorf("запрос: %s %s", path, auth)
	}
	if len(got) != 2 || got[0].UUID != "u-1" || got[0].Name != "Европа" || got[0].Members != 12 || got[1].Name != "Азия" {
		t.Errorf("сквады: %+v", got)
	}
}

func TestSquadsPanelDown(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(502) }))
	defer srv.Close()
	if _, err := panel.New(srv.URL, "tok", nil).Squads(context.Background()); err == nil {
		t.Error("ответ 502 принят за успех")
	}
}
