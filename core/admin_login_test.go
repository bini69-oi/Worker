package core_test

import (
	"context"
	"errors"
	"testing"

	"github.com/bini69-oi/worker/core"
)

const ownerPass = "correct horse battery"

func startLogin(t *testing.T, e *env) core.LoginStep {
	t.Helper()
	ctx := context.Background()
	if err := e.c.SetOwnerPassword(ctx, ownerPass); err != nil {
		t.Fatal(err)
	}
	step, err := e.c.LoginStart(ctx, owner, ownerPass, core.LoginInfo{IP: "203.0.113.5", Agent: "test"})
	if err != nil {
		t.Fatal(err)
	}
	return step
}

func TestLoginPollStates(t *testing.T) {
	ctx := context.Background()
	info := core.LoginInfo{IP: "203.0.113.5", Agent: "test"}
	t.Run("ждёт, потом вход", func(t *testing.T) {
		e := newEnv(t)
		step := startLogin(t, e)
		st, token, _, err := e.c.LoginPoll(ctx, step.ConfirmID, step.Nonce, info)
		if err != nil || st != core.LoginPending || token != "" {
			t.Fatalf("до нажатия: %q %q %v", st, token, err)
		}
		if err := e.c.AnswerLogin(ctx, step.ConfirmID, owner, step.Code); err != nil {
			t.Fatal(err)
		}
		st, token, sess, err := e.c.LoginPoll(ctx, step.ConfirmID, step.Nonce, info)
		if err != nil || st != core.LoginOK || token == "" || sess.AdminTg != owner {
			t.Fatalf("после нажатия: %q %v", st, err)
		}
		if _, _, err := e.c.SessionAdmin(ctx, token, info); err != nil {
			t.Fatalf("сессия не работает: %v", err)
		}
	})
	t.Run("чужой код это отказ", func(t *testing.T) {
		e := newEnv(t)
		step := startLogin(t, e)
		if err := e.c.AnswerLogin(ctx, step.ConfirmID, owner, "00"); err != nil {
			t.Fatal(err)
		}
		st, token, _, err := e.c.LoginPoll(ctx, step.ConfirmID, step.Nonce, info)
		if err != nil || st != core.LoginDenied || token != "" {
			t.Fatalf("%q %q %v", st, token, err)
		}
	})
	t.Run("чужой ключ браузера", func(t *testing.T) {
		e := newEnv(t)
		step := startLogin(t, e)
		_, _, _, err := e.c.LoginPoll(ctx, step.ConfirmID, "чужой", info)
		if !errors.Is(err, core.ErrNotConfirmed) {
			t.Fatalf("ждали ErrNotConfirmed: %v", err)
		}
	})
	t.Run("устарел", func(t *testing.T) {
		e := newEnv(t)
		step := startLogin(t, e)
		e.clock.Add(3 * 60 * 1e9)
		st, _, _, err := e.c.LoginPoll(ctx, step.ConfirmID, step.Nonce, info)
		if err != nil || st != core.LoginExpired {
			t.Fatalf("%q %v", st, err)
		}
	})
}
