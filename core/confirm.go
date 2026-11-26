package core

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"sync"
	"time"
)

// confirmTTL это срок, за который админ должен нажать «Да» или «Нет».
const confirmTTL = 2 * time.Minute

// Confirm это подтверждение кнопкой в Telegram: вход или опасное действие.
type Confirm struct {
	ID         int64
	AdminTg    int64
	Kind       string
	Payload    []byte
	CreatedAt  time.Time
	ExpiresAt  time.Time
	Answer     string // yes, no или пусто, пока не нажали
	AnsweredAt time.Time
	UsedAt     time.Time
}

// ConfirmTask это сообщение админу с кнопками «Да» и «Нет».
type ConfirmTask struct {
	ConfirmID int64             `json:"confirm_id"`
	AdminTgID int64             `json:"admin_tg_id"`
	Kind      string            `json:"kind"`
	Vars      map[string]string `json:"vars,omitempty"`
}

// KindNotifyConfirm шлёт админу подтверждение с кнопками.
const KindNotifyConfirm = "notify.confirm"

// newConfirm создаёт подтверждение на 2 минуты и ставит в очередь сообщение админу.
func (c *Core) newConfirm(tx Tx, adminTg int64, kind string, payload []byte, vars map[string]string, now time.Time) (Confirm, error) {
	cf := Confirm{AdminTg: adminTg, Kind: kind, Payload: payload, CreatedAt: now, ExpiresAt: now.Add(confirmTTL)}
	var err error
	if cf.ID, err = tx.CreateConfirm(cf); err != nil {
		return cf, err
	}
	body, err := json.Marshal(ConfirmTask{ConfirmID: cf.ID, AdminTgID: adminTg, Kind: kind, Vars: vars})
	if err != nil {
		return cf, err
	}
	_, err = tx.Enqueue(KindNotifyConfirm, body, now)
	return cf, err
}

// AnswerConfirm принимает нажатие кнопки. Нажать может только тот админ, которому пришло сообщение,
// и только один раз, пока не истекли 2 минуты. «Нет» шлёт владельцу тревогу. «Да» на опасное действие
// выполняет его. Вход «Да» так не подтверждается: только кнопкой с кодом через AnswerLogin.
func (c *Core) AnswerConfirm(ctx context.Context, confirmID, pressedBy int64, yes bool) error {
	_, err := c.answer(ctx, confirmID, pressedBy, func(cf Confirm) (bool, error) {
		if yes && cf.Kind == "login" {
			return false, ErrInvalid
		}
		return yes, nil
	})
	return err
}

// AnswerLogin принимает нажатие кнопки с кодом на сообщении о входе. Код тот, что показал браузер:
// вход подтверждён. Код другой: это «Нет», то есть тревога и блок адреса, с которого входили.
func (c *Core) AnswerLogin(ctx context.Context, confirmID, pressedBy int64, code string) error {
	_, err := c.AnswerLoginCode(ctx, confirmID, pressedBy, code)
	return err
}

// AnswerLoginCode как AnswerLogin, но говорит, подтверждён ли вход: бот пишет итог в сообщении.
func (c *Core) AnswerLoginCode(ctx context.Context, confirmID, pressedBy int64, code string) (bool, error) {
	return c.answer(ctx, confirmID, pressedBy, func(cf Confirm) (bool, error) {
		if cf.Kind != "login" {
			return false, ErrInvalid
		}
		var p loginPayload
		if err := json.Unmarshal(cf.Payload, &p); err != nil {
			return false, err
		}
		return p.Code != "" && subtle.ConstantTimeCompare([]byte(code), []byte(p.Code)) == 1, nil
	})
}

// answer записывает ответ на подтверждение. decide по подтверждению говорит, «Да» это или «Нет».
// Отдаёт, каким вышел ответ.
func (c *Core) answer(ctx context.Context, confirmID, pressedBy int64, decide func(Confirm) (bool, error)) (bool, error) {
	var cf Confirm
	yes := false
	err := c.tx(ctx, func(tx Tx) error {
		now := c.clock.Now()
		var err error
		if cf, err = tx.Confirm(confirmID); err != nil {
			return err
		}
		if cf.AdminTg != pressedBy {
			return ErrForbidden
		}
		if yes, err = decide(cf); err != nil {
			return err
		}
		answer := "no"
		if yes {
			answer = "yes"
		}
		ok, err := tx.AnswerConfirm(confirmID, answer, now)
		if err != nil {
			return err
		}
		if !ok {
			return ErrConfirmGone
		}
		if !yes {
			vars := map[string]string{"admin": itoa(cf.AdminTg), "kind": cf.Kind}
			var p loginPayload
			if cf.Kind == "login" && json.Unmarshal(cf.Payload, &p) == nil && p.IP != "" {
				vars["ip"], vars["agent"] = p.IP, p.Agent
				// Админ сказал «не я»: с этого адреса вход для него закрыт на час, даже с верным паролем.
				if err := tx.SetLimit("login:"+itoa(cf.AdminTg)+"@"+p.IP, loginTries, now.Add(loginMaxPause)); err != nil {
					return err
				}
			}
			return c.alert(tx, "admin.confirm_rejected", vars, now)
		}
		if cf.Kind == "login" {
			return nil // вход заканчивает браузер через LoginFinish
		}
		if ok, err := tx.UseConfirm(confirmID, now); err != nil || !ok {
			return ErrConfirmGone
		}
		return c.audit(ctx, tx, pressedBy, "approve:"+cf.Kind, itoa(cf.ID), nil, now)
	})
	if err != nil || !yes || cf.Kind == "login" {
		c.secrets.drop(confirmID, !yes)
		return yes && err == nil, err
	}
	return true, c.runApproved(ctx, cf)
}

// pending держит в памяти новые секреты, которые ждут «Да» владельца. В базу открытый секрет не пишется,
// а после перезапуска ожидание просто пропадает, и владелец повторяет действие.
type pending struct {
	mu   sync.Mutex
	vals map[int64]pendingSecret
}

type pendingSecret struct {
	value   string
	expires time.Time
}

// put запоминает секрет и заодно выбрасывает просроченные, чтобы неподтверждённые ключи не жили в памяти.
func (p *pending) put(id int64, value string, expires, now time.Time) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.vals == nil {
		p.vals = map[int64]pendingSecret{}
	}
	for k, v := range p.vals {
		if now.After(v.expires) {
			delete(p.vals, k)
		}
	}
	p.vals[id] = pendingSecret{value, expires}
}

func (p *pending) take(id int64, now time.Time) (string, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	v, ok := p.vals[id]
	delete(p.vals, id)
	if !ok || now.After(v.expires) {
		return "", false
	}
	return v.value, true
}

func (p *pending) drop(id int64, really bool) {
	if !really {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.vals, id)
}
