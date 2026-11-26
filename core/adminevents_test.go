package core_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/bini69-oi/worker/core"
)

func adminTasks(t *testing.T, e *env) []core.AdminTask {
	t.Helper()
	var out []core.AdminTask
	for _, task := range e.st.Tasks() {
		if task.Kind != core.KindNotifyAdmin {
			continue
		}
		var n core.AdminTask
		if err := json.Unmarshal(task.Payload, &n); err != nil {
			t.Fatal(err)
		}
		out = append(out, n)
	}
	return out
}

func TestRegisterSendsNewUserEvent(t *testing.T) {
	e := newEnv(t)
	if _, err := e.c.Register(context.Background(), 2002, "petr", "ru"); err != nil {
		t.Fatal(err)
	}
	got := adminTasks(t, e)
	if len(got) != 1 || got[0].Topic != core.TopicNew || got[0].Key != "admin.new_user" || got[0].Vars["username"] != "petr" {
		t.Fatalf("событие о новом абоненте: %+v", got)
	}

	// Повторный /start того же человека не новый абонент.
	if _, err := e.c.Register(context.Background(), 2002, "petr", "ru"); err != nil {
		t.Fatal(err)
	}
	if n := len(adminTasks(t, e)); n != 1 {
		t.Fatalf("повторная регистрация дала событие: %d", n)
	}
}

func TestTrialSendsEvent(t *testing.T) {
	e := newEnv(t)
	for k, v := range map[string]string{"trial.enabled": "true", "trial.days": "3"} {
		if err := e.cfg.Set(k, v); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := e.c.Trial(context.Background(), e.user.ID); err != nil {
		t.Fatal(err)
	}
	got := adminTasks(t, e)
	if len(got) != 1 || got[0].Topic != core.TopicTrials || got[0].Key != "admin.trial" || got[0].Vars["days"] != "3" {
		t.Fatalf("событие о пробном: %+v", got)
	}
}
