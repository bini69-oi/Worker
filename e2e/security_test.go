package e2e

import (
	"context"
	"testing"

	"github.com/bini69-oi/worker/core"
)

// TestSecurityJourney: владелец задаёт пароль, входит в два шага, меняет ключ кассы через «Да» в Telegram.
// Угнанная сессия без «Да» ключ не меняет. Всё видно в журнале действий и в тревогах.
func TestSecurityJourney(t *testing.T) {
	w := newWorld(t)
	ctx := core.WithIP(context.Background(), "10.0.0.1")
	if err := w.c.SetOwnerPassword(ctx, "owner-password-1"); err != nil {
		t.Fatal(err)
	}

	id, nonce, err := w.c.LoginStart(ctx, owner, "owner-password-1", core.LoginInfo{IP: "10.0.0.1", Agent: "Safari"})
	if err != nil {
		t.Fatal(err)
	}
	w.process(t)
	if len(w.notes.ToConfirm) != 1 || w.notes.ToConfirm[0].ConfirmID != id {
		t.Fatalf("подтверждение входа не ушло в Telegram: %+v", w.notes.ToConfirm)
	}
	if err := w.c.AnswerConfirm(ctx, id, owner, true); err != nil {
		t.Fatal(err)
	}
	token, _, err := w.c.LoginFinish(ctx, id, nonce, core.LoginInfo{IP: "10.0.0.1", Agent: "Safari"})
	if err != nil {
		t.Fatal(err)
	}
	if a, _, err := w.c.SessionAdmin(ctx, token); err != nil || !a.Owner {
		t.Fatalf("сессия владельца: %+v %v", a, err)
	}

	// Угнанная сессия просит сменить ключ, но владелец не нажал «Да».
	stolen, err := w.c.RequestSecret(ctx, owner, "yookassa.secret_key", "attacker_key")
	if err != nil {
		t.Fatal(err)
	}
	if w.cfg.Secret("yookassa.secret_key") != "" {
		t.Fatal("ключ кассы сменился без «Да»")
	}
	if err := w.c.AnswerConfirm(ctx, stolen, owner, false); err != nil {
		t.Fatal(err)
	}

	// Настоящая смена ключа с «Да».
	genuine, err := w.c.RequestSecret(ctx, owner, "yookassa.secret_key", "live_real_key")
	if err != nil {
		t.Fatal(err)
	}
	if err := w.c.AnswerConfirm(ctx, genuine, owner, true); err != nil {
		t.Fatal(err)
	}
	if got := w.cfg.Secret("yookassa.secret_key"); got != "live_real_key" {
		t.Fatalf("ключ после «Да»: %q", got)
	}
	w.process(t)

	want := map[string]bool{"admin.password_set": false, "admin.login": false, "admin.confirm_rejected": false, "admin.changed": false}
	for _, a := range w.notes.ToAdmins {
		if _, ok := want[a.Key]; ok && a.Topic == core.TopicSecurity {
			want[a.Key] = true
		}
	}
	for k, got := range want {
		if !got {
			t.Errorf("нет тревоги %s", k)
		}
	}

	rows, err := w.db.R.Query("SELECT action, ip FROM audit_log ORDER BY id")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	seen := map[string]string{}
	for rows.Next() {
		var action, ip string
		if err := rows.Scan(&action, &ip); err != nil {
			t.Fatal(err)
		}
		seen[action] = ip
	}
	for _, action := range []string{"owner.password", "login", "request:secret", "approve:secret"} {
		if _, ok := seen[action]; !ok {
			t.Errorf("в журнале действий нет %s: %v", action, seen)
		}
	}
	if seen["login"] != "10.0.0.1" {
		t.Errorf("у входа в журнале нет IP: %v", seen)
	}
	if _, err := w.db.W.Exec("DELETE FROM audit_log"); err == nil {
		t.Error("журнал действий удалось стереть")
	}
}
