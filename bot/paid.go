package bot

import (
	"context"
	"errors"

	"github.com/bini69-oi/worker/core"
)

// showPaid показывает статус платежа после возврата из кассы. Чужой или несуществующий платёж: меню.
func (b *Bot) showPaid(ctx context.Context, r *req, id int64) error {
	p, err := b.core.MyPayment(ctx, r.user.ID, id)
	if errors.Is(err, core.ErrNotFound) {
		return b.showMenu(ctx, r)
	}
	if err != nil {
		return err
	}
	vars := map[string]string{"amount": money(p.Amount)}
	s := Screen{Text: b.t(r, "pay.status."+string(p.Status), vars)}
	if p.Status == core.PayPending {
		s.Rows = append(s.Rows, []Button{b.btn(r, "btn.check_pay", data("pchk", p.ID))})
	}
	s.Rows = append(s.Rows, b.back(r, "m"))
	return b.show(ctx, r, s)
}

func (b *Bot) onCheckPay(ctx context.Context, r *req, d Data) error {
	id, ok := d.Int(0)
	if !ok {
		return core.ErrInvalid
	}
	return b.showPaid(ctx, r, id)
}
